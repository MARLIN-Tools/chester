package chester

import (
	"context"
	"math"
	"time"
)

const (
	Inf            = 1_000_000_000
	MateScore      = 1_000_000
	maxSearchDepth = 64
	maxSearchPly   = 128

	ttFlagExact = iota
	ttFlagLowerBound
	ttFlagUpperBound

	scoreTTMove    = 1_000_000_000
	scorePromotion = 200_000_000
	scoreCapture   = 100_000_000
	scoreKiller1   = 90_000_000
	scoreKiller2   = 80_000_000
)

type SearchLimits struct {
	MaxDepth int
	SoftTime time.Duration
	HardTime time.Duration
	Infinite bool
}

type SearchOptions struct {
	TranspositionTable *TranspositionTable
	UseOpeningBook     bool
}

type SearchUpdate struct {
	Depth    int
	SelDepth int
	Score    int
	Nodes    uint64
	NPS      uint64
	Time     time.Duration
	PV       []Move
	Best     Move
}

type searchController struct {
	ctx    context.Context
	root   Position
	limits SearchLimits
	opts   SearchOptions
	tt     *TranspositionTable

	start        time.Time
	softDeadline time.Time
	hardDeadline time.Time

	nodes    uint64
	seldepth int

	killers [maxSearchPly][2]Move
	history [2][64][64]int

	bestMove     Move
	rootFallback Move
}

func SearchBestMove(ctx context.Context, p *Position, limits SearchLimits, options SearchOptions) <-chan SearchUpdate {
	updates := make(chan SearchUpdate)

	go func() {
		defer close(updates)

		controller := newSearchController(ctx, p, limits, options)
		controller.search(updates)
	}()

	return updates
}

func newSearchController(ctx context.Context, p *Position, limits SearchLimits, options SearchOptions) *searchController {
	if limits.MaxDepth <= 0 || limits.MaxDepth > maxSearchDepth {
		limits.MaxDepth = maxSearchDepth
	}

	controller := &searchController{
		ctx:    ctx,
		root:   *p,
		limits: limits,
		opts:   options,
		tt:     options.TranspositionTable,
		start:  time.Now(),
	}

	if limits.SoftTime > 0 {
		controller.softDeadline = controller.start.Add(limits.SoftTime)
	}
	if limits.HardTime > 0 {
		controller.hardDeadline = controller.start.Add(limits.HardTime)
	}

	return controller
}

func (s *searchController) search(updates chan<- SearchUpdate) {
	if s.opts.UseOpeningBook {
		if entries, ok := book[s.root.Hash()]; ok {
			move := pickMove(entries)
			updates <- SearchUpdate{
				Depth: 1,
				Time:  s.elapsed(),
				PV:    []Move{move},
				Best:  move,
			}
			return
		}
	}

	rootMoves := make([]Move, 0, 218)
	rootMoves, inCheck := LegalMoves(rootMoves, &s.root)
	if len(rootMoves) == 0 {
		score := 0
		if inCheck {
			score = -MateScore
		}
		updates <- SearchUpdate{
			Score: score,
			Time:  s.elapsed(),
		}
		return
	}

	s.rootFallback = rootMoves[0]

	var lastComplete SearchUpdate
	for depth := 1; depth <= s.limits.MaxDepth; depth++ {
		score, bestMove, complete := s.searchRoot(depth)
		if !complete {
			break
		}

		if bestMove != 0 {
			s.bestMove = bestMove
		}

		pv := s.extractPV(depth)
		if len(pv) == 0 && bestMove != 0 {
			pv = []Move{bestMove}
		}

		lastComplete = SearchUpdate{
			Depth:    depth,
			SelDepth: s.seldepth,
			Score:    score,
			Nodes:    s.nodes,
			NPS:      s.nps(),
			Time:     s.elapsed(),
			PV:       pv,
			Best:     bestMove,
		}
		updates <- lastComplete

		if s.shouldStopSoft() {
			break
		}
	}

	if lastComplete.Best != 0 {
		return
	}

	fallback := s.bestMove
	if fallback == 0 {
		fallback = s.rootFallback
	}
	if fallback != 0 {
		updates <- SearchUpdate{
			SelDepth: s.seldepth,
			Nodes:    s.nodes,
			NPS:      s.nps(),
			Time:     s.elapsed(),
			PV:       []Move{fallback},
			Best:     fallback,
		}
	}
}

func (s *searchController) searchRoot(depth int) (int, Move, bool) {
	if !s.enterNode(0) {
		return 0, 0, false
	}

	moves := make([]Move, 0, 218)
	moves, inCheck := LegalMoves(moves, &s.root)
	if len(moves) == 0 {
		if inCheck {
			return -MateScore, 0, true
		}
		return 0, 0, true
	}

	bestMove := s.bestMove
	if bestMove == 0 {
		bestMove = s.rootFallback
	}

	if entry, ok := s.tt.lookup(s.root.Hash()); ok {
		if entry.bestMove != 0 {
			bestMove = entry.bestMove
		}
	}

	s.orderMoves(&s.root, moves, bestMove, 0)

	alpha := -Inf
	beta := Inf
	originalAlpha := alpha
	bestScore := math.MinInt
	best := Move(0)

	for _, move := range moves {
		child := s.root
		child.Do(move)

		score, ok := s.negamax(&child, -beta, -alpha, depth-1, 1)
		if !ok {
			return bestScore, best, false
		}
		score = -score

		if best == 0 || score > bestScore {
			bestScore = score
			best = move
			s.bestMove = move
		}

		if score > alpha {
			alpha = score
		}

		if alpha >= beta {
			break
		}
	}

	if best != 0 {
		s.storeTT(s.root.Hash(), depth, 0, bestScore, originalAlpha, beta, best)
	}

	return bestScore, best, true
}

func (s *searchController) negamax(p *Position, alpha, beta, depth, ply int) (int, bool) {
	if !s.enterNode(ply) {
		return 0, false
	}

	hash := p.Hash()
	originalAlpha := alpha
	ttMove := Move(0)

	if entry, ok := s.tt.lookup(hash); ok {
		ttMove = entry.bestMove
		if int(entry.depth) >= depth {
			score := fromTTScore(entry.score, ply)
			switch entry.flag {
			case ttFlagExact:
				return score, true
			case ttFlagLowerBound:
				if score > alpha {
					alpha = score
				}
			case ttFlagUpperBound:
				if score < beta {
					beta = score
				}
			}
			if alpha >= beta {
				return score, true
			}
		}
	}

	if depth == 0 {
		return s.quiescence(p, alpha, beta, ply)
	}

	moves := make([]Move, 0, 218)
	moves, inCheck := LegalMoves(moves, p)
	if len(moves) == 0 {
		if inCheck {
			return -MateScore + ply, true
		}
		return 0, true
	}

	s.orderMoves(p, moves, ttMove, ply)

	bestScore := math.MinInt
	bestMove := Move(0)

	for _, move := range moves {
		child := *p
		child.Do(move)

		score, ok := s.negamax(&child, -beta, -alpha, depth-1, ply+1)
		if !ok {
			return 0, false
		}
		score = -score

		if bestMove == 0 || score > bestScore {
			bestScore = score
			bestMove = move
		}

		if score > alpha {
			alpha = score
		}

		if alpha >= beta {
			if !isTacticalMove(p, move) {
				s.recordKiller(ply, move)
				s.recordHistory(p.Active(), move, depth)
			}
			break
		}
	}

	if bestMove != 0 {
		s.storeTT(hash, depth, ply, bestScore, originalAlpha, beta, bestMove)
	}

	return bestScore, true
}

func (s *searchController) quiescence(p *Position, alpha, beta, ply int) (int, bool) {
	if !s.enterNode(ply) {
		return 0, false
	}

	captures := make([]Move, 0, 32)
	captures, inCheck := CaptureMoves(captures, p)

	if !inCheck {
		standPat := evalPesto(p)
		if standPat >= beta {
			return beta, true
		}
		if standPat > alpha {
			alpha = standPat
		}
		if len(captures) == 0 {
			return alpha, true
		}
	} else {
		captures = make([]Move, 0, 218)
		captures, _ = LegalMoves(captures, p)
		if len(captures) == 0 {
			return -MateScore + ply, true
		}
	}

	s.orderTacticalMoves(p, captures)

	for _, move := range captures {
		child := *p
		child.Do(move)

		score, ok := s.quiescence(&child, -beta, -alpha, ply+1)
		if !ok {
			return 0, false
		}
		score = -score

		if score >= beta {
			return beta, true
		}
		if score > alpha {
			alpha = score
		}
	}

	return alpha, true
}

func (s *searchController) extractPV(depth int) []Move {
	if s.tt == nil {
		return nil
	}

	pv := make([]Move, 0, depth)
	position := s.root
	seen := make(map[uint64]struct{}, depth)

	for i := 0; i < depth; i++ {
		hash := position.Hash()
		if _, ok := seen[hash]; ok {
			break
		}
		seen[hash] = struct{}{}

		entry, ok := s.tt.lookup(hash)
		if !ok || entry.bestMove == 0 {
			break
		}

		move, ok := s.isLegalMove(&position, entry.bestMove)
		if !ok {
			break
		}

		pv = append(pv, move)
		position.Do(move)
	}

	return pv
}

func (s *searchController) isLegalMove(p *Position, move Move) (Move, bool) {
	moves := make([]Move, 0, 218)
	moves, _ = LegalMoves(moves, p)
	for _, legal := range moves {
		if legal == move {
			return legal, true
		}
	}
	return 0, false
}

func (s *searchController) storeTT(hash uint64, depth, ply, score, alpha, beta int, bestMove Move) {
	if s.tt == nil || bestMove == 0 {
		return
	}

	flag := ttFlagExact
	switch {
	case score <= alpha:
		flag = ttFlagUpperBound
	case score >= beta:
		flag = ttFlagLowerBound
	}

	s.tt.store(ttEntry{
		key:      hash,
		bestMove: bestMove,
		score:    toTTScore(score, ply),
		depth:    int16(depth),
		flag:     uint8(flag),
	})
}

func (s *searchController) enterNode(ply int) bool {
	s.nodes++
	if ply > s.seldepth {
		s.seldepth = ply
	}

	if s.nodes&1023 != 0 {
		return true
	}

	return !s.shouldStopHard()
}

func (s *searchController) shouldStopSoft() bool {
	if s.limits.Infinite || s.softDeadline.IsZero() {
		return false
	}
	return !time.Now().Before(s.softDeadline)
}

func (s *searchController) shouldStopHard() bool {
	if s.ctx.Err() != nil {
		return true
	}
	if s.limits.Infinite || s.hardDeadline.IsZero() {
		return false
	}
	return !time.Now().Before(s.hardDeadline)
}

func (s *searchController) elapsed() time.Duration {
	return time.Since(s.start)
}

func (s *searchController) nps() uint64 {
	elapsed := s.elapsed()
	if elapsed <= 0 {
		return 0
	}
	return uint64(float64(s.nodes) / elapsed.Seconds())
}

func (s *searchController) orderMoves(p *Position, moves []Move, ttMove Move, ply int) {
	scores := make([]int, len(moves))
	for i, move := range moves {
		scores[i] = s.scoreMove(p, move, ttMove, ply)
	}
	sortMoves(moves, scores)
}

func (s *searchController) orderTacticalMoves(p *Position, moves []Move) {
	scores := make([]int, len(moves))
	for i, move := range moves {
		scores[i] = tacticalMoveScore(p, move)
	}
	sortMoves(moves, scores)
}

func (s *searchController) scoreMove(p *Position, move, ttMove Move, ply int) int {
	if move == ttMove {
		return scoreTTMove
	}

	if isTacticalMove(p, move) {
		return tacticalMoveScore(p, move)
	}

	if ply < maxSearchPly {
		if move == s.killers[ply][0] {
			return scoreKiller1
		}
		if move == s.killers[ply][1] {
			return scoreKiller2
		}
	}

	return s.history[p.Active()][move.From()][move.To()]
}

func (s *searchController) recordKiller(ply int, move Move) {
	if ply >= maxSearchPly {
		return
	}
	if s.killers[ply][0] == move {
		return
	}
	s.killers[ply][1] = s.killers[ply][0]
	s.killers[ply][0] = move
}

func (s *searchController) recordHistory(color Color, move Move, depth int) {
	value := s.history[color][move.From()][move.To()] + depth*depth
	if value > 1_000_000 {
		value = 1_000_000
	}
	s.history[color][move.From()][move.To()] = value
}

func sortMoves(moves []Move, scores []int) {
	for i := 0; i < len(moves); i++ {
		best := i
		for j := i + 1; j < len(moves); j++ {
			if scores[j] > scores[best] {
				best = j
			}
		}
		if best != i {
			moves[i], moves[best] = moves[best], moves[i]
			scores[i], scores[best] = scores[best], scores[i]
		}
	}
}

func isTacticalMove(p *Position, move Move) bool {
	return move.IsPromotion() || capturedPieceForMove(p, move) != Empty
}

func tacticalMoveScore(p *Position, move Move) int {
	score := 0
	if move.IsPromotion() {
		score += scorePromotion + pieceValue(move.PromoPiece())
	}

	captured := capturedPieceForMove(p, move)
	if captured != Empty {
		score += scoreCapture + pieceValue(captured)*16 - pieceValue(p.Get(move.From()))
	}

	return score
}

func capturedPieceForMove(p *Position, move Move) Piece {
	captured := p.Get(move.To())
	if captured != Empty {
		return captured
	}
	if p.Get(move.From()) == Pawn && move.To() == p.EnPassantTarget() {
		return Pawn
	}
	return Empty
}

func pieceValue(piece Piece) int {
	switch piece {
	case Pawn:
		return 100
	case Knight:
		return 320
	case Bishop:
		return 330
	case Rook:
		return 500
	case Queen:
		return 900
	default:
		return 0
	}
}

func toTTScore(score, ply int) int {
	if score >= MateScore-maxSearchPly {
		return score + ply
	}
	if score <= -MateScore+maxSearchPly {
		return score - ply
	}
	return score
}

func fromTTScore(score, ply int) int {
	if score >= MateScore-maxSearchPly {
		return score - ply
	}
	if score <= -MateScore+maxSearchPly {
		return score + ply
	}
	return score
}
