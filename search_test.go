package chester

import (
	"context"
	"testing"
	"time"
)

func TestSearchInterruptedReturnsLegalMove(t *testing.T) {
	pos, err := ParseFEN("r1bq1rk1/ppp2ppp/2n2n2/2bp4/2B5/2NP1N2/PPP2PPP/R1BQ1RK1 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	updates := SearchBestMove(ctx, pos, SearchLimits{MaxDepth: 8}, SearchOptions{
		TranspositionTable: NewTranspositionTable(8),
		UseOpeningBook:     false,
	})

	best := Move(0)
	for update := range updates {
		if update.Best != 0 {
			best = update.Best
		}
	}

	if best == 0 {
		t.Fatal("expected interrupted search to return a legal move")
	}

	moves := make([]Move, 0, 218)
	moves, _ = LegalMoves(moves, pos)

	found := false
	for _, move := range moves {
		if move == best {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("returned move %s is not legal", best.String())
	}
}

func TestSearchReportsIncreasingDepths(t *testing.T) {
	pos, err := ParseFEN("r1bq1rk1/ppp2ppp/2n2n2/2bp4/2B5/2NP1N2/PPP2PPP/R1BQ1RK1 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseFEN: %v", err)
	}

	updates := SearchBestMove(context.Background(), pos, SearchLimits{MaxDepth: 3}, SearchOptions{
		TranspositionTable: NewTranspositionTable(8),
		UseOpeningBook:     false,
	})

	depths := []int{}
	for update := range updates {
		if update.Depth > 0 {
			depths = append(depths, update.Depth)
		}
	}

	if len(depths) < 3 {
		t.Fatalf("expected at least 3 completed iterations, got %v", depths)
	}

	for i, depth := range depths[:3] {
		expected := i + 1
		if depth != expected {
			t.Fatalf("expected depth progression [1 2 3], got %v", depths[:3])
		}
	}
}

func TestSearchHandlesLateEndgameCrashPosition(t *testing.T) {
	pos, err := ParseFEN("8/8/8/1p6/8/2Q2P1P/PP4P1/1k1K4 b - - 3 47")
	if err != nil {
		t.Fatalf("ParseFEN: %v", err)
	}

	updates := SearchBestMove(context.Background(), pos, SearchLimits{MaxDepth: 4}, SearchOptions{
		TranspositionTable: NewTranspositionTable(8),
		UseOpeningBook:     false,
	})

	best := Move(0)
	for update := range updates {
		if update.Best != 0 {
			best = update.Best
		}
	}

	if best == 0 {
		t.Fatal("expected a legal bestmove for the late endgame regression position")
	}

	moves := make([]Move, 0, 218)
	moves, _ = LegalMoves(moves, pos)

	found := false
	for _, move := range moves {
		if move == best {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("returned move %s is not legal", best.String())
	}
}
