package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bluescreen10/chester"
)

const (
	defaultHashMB       = 64
	defaultMoveOverhead = 10 * time.Millisecond
	defaultThreads      = 1
)

type UCIServer struct {
	mutex sync.Mutex

	pos          *chester.Position
	tt           *chester.TranspositionTable
	hashMB       int
	ownBook      bool
	moveOverhead time.Duration
	threads      int

	currentCancel func()
	currentDone   chan struct{}

	isCPUProfiling bool
	CPUProfileFile *os.File
	isDebugLogging bool
}

type goOptions struct {
	depth     int
	softLimit time.Duration
	hardLimit time.Duration
	infinite  bool
}

func startUCI() {
	pos, _ := chester.ParseFEN(chester.DefaultFEN)
	uci := &UCIServer{
		pos:          pos,
		tt:           chester.NewTranspositionTable(defaultHashMB),
		hashMB:       defaultHashMB,
		ownBook:      false,
		moveOverhead: defaultMoveOverhead,
		threads:      defaultThreads,
	}
	uci.Start()
}

func (s *UCIServer) Start() {
	s.info("starting uci server...")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		s.stopSearch(true)
		os.Exit(0)
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		s.debug("received: %s", line)

		args := strings.Fields(line)
		if len(args) == 0 {
			continue
		}

		switch args[0] {
		case "quit":
			s.stopSearch(true)
			s.info("quitting uci server...")
			return
		case "uci":
			s.handleUCI()
		case "ucinewgame":
			s.handleUCINewGame()
		case "setoption":
			s.handleSetOption(args[1:])
		case "position":
			s.handlePosition(args[1:])
		case "go":
			s.handleGo(args[1:])
		case "stop":
			s.handleStop()
		case "ponderhit":
			// Ponder is not implemented.
		case "isready":
			s.handleIsReady()
		case "perft":
			s.handlePerft(args[1:])
		case "cpuprofile":
			s.handleCPUProfile(args[1:])
		case "debug":
			s.handleDebug(args[1:])
		default:
			s.debug("ignoring unknown command: %s", args[0])
		}
	}
}

func (s *UCIServer) Write(msg []byte) (int, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return os.Stdout.Write(msg)
}

func (s *UCIServer) WriteString(msg string, args ...any) {
	s.Write([]byte(fmt.Sprintf(msg+"\n", args...)))
}

func (s *UCIServer) debug(msg string, args ...any) {
	if !s.isDebugLogging {
		return
	}
	s.info("debug: "+msg, args...)
}

func (s *UCIServer) info(msg string, args ...any) {
	s.WriteString("info string "+msg, args...)
}

func (s *UCIServer) error(msg string, args ...any) {
	s.info("error: "+msg, args...)
}

func (s *UCIServer) handleUCI() {
	s.WriteString("id name %s", BotName)
	s.WriteString("id author %s", Author)
	s.WriteString("option name Hash type spin default %d min 1 max 1048576", defaultHashMB)
	s.WriteString("option name OwnBook type check default false")
	s.WriteString("option name Move Overhead type spin default %d min 0 max 5000", defaultMoveOverhead/time.Millisecond)
	s.WriteString("option name Threads type spin default %d min 1 max 1", defaultThreads)
	s.WriteString("uciok")
}

func (s *UCIServer) handleUCINewGame() {
	s.stopSearch(true)
	s.resetPosition()
	if s.tt != nil {
		s.tt.Clear()
	}
}

func (s *UCIServer) handleSetOption(args []string) {
	name, value := parseOption(args)
	switch strings.ToLower(name) {
	case "hash":
		hashMB, err := strconv.Atoi(value)
		if err != nil || hashMB < 1 {
			s.error("invalid Hash value: %s", value)
			return
		}
		s.hashMB = hashMB
		s.tt.Resize(hashMB)
	case "ownbook":
		s.ownBook = parseBool(value)
	case "move overhead":
		overhead, err := strconv.Atoi(value)
		if err != nil || overhead < 0 {
			s.error("invalid Move Overhead value: %s", value)
			return
		}
		s.moveOverhead = time.Duration(overhead) * time.Millisecond
	case "threads":
		threads, err := strconv.Atoi(value)
		if err != nil || threads < 1 {
			s.error("invalid Threads value: %s", value)
			return
		}
		s.threads = 1
	default:
		s.debug("ignoring unsupported option %q", name)
	}
}

func (s *UCIServer) handlePosition(args []string) {
	if len(args) < 1 {
		s.error("position command requires at least 1 argument")
		return
	}

	switch args[0] {
	case "startpos":
		s.resetPosition()
		if len(args) > 1 {
			args = args[1:]
		} else {
			args = nil
		}
	case "fen":
		var i int
		for i = 1; i < len(args); i++ {
			if args[i] == "moves" {
				break
			}
		}

		fen := strings.Join(args[1:i], " ")
		pos, err := chester.ParseFEN(fen)
		if err != nil {
			s.error("error parsing fen: %s", err)
			return
		}
		s.pos = pos
		args = args[i:]
	default:
		s.error("unknown position argument: %s", args[0])
		return
	}

	if len(args) > 0 && args[0] == "moves" {
		for _, moveText := range args[1:] {
			moveText = strings.TrimSpace(moveText)
			if moveText == "" {
				continue
			}

			move, err := chester.ParseMove(moveText, s.pos)
			if err != nil {
				s.error("error parsing move: %s", err)
				return
			}
			s.pos.Do(move)
		}
	}
}

func (s *UCIServer) handleGo(args []string) {
	s.stopSearch(true)

	options := s.parseGoOptions(args)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	s.currentCancel = cancel
	s.currentDone = done

	go func() {
		defer close(done)

		position := *s.pos
		updates := chester.SearchBestMove(ctx, &position, chester.SearchLimits{
			MaxDepth: options.depth,
			SoftTime: options.softLimit,
			HardTime: options.hardLimit,
			Infinite: options.infinite,
		}, chester.SearchOptions{
			TranspositionTable: s.tt,
			UseOpeningBook:     s.ownBook,
		})

		bestMove := chester.Move(0)
		for update := range updates {
			if update.Best != 0 {
				bestMove = update.Best
			}
			if update.Depth > 0 {
				s.emitSearchInfo(update)
			}
		}

		s.WriteString("bestmove %s", moveToUCI(bestMove))
	}()
}

func (s *UCIServer) emitSearchInfo(update chester.SearchUpdate) {
	s.WriteString(
		"info depth %d seldepth %d score cp %d time %d nodes %d nps %d pv %s",
		update.Depth,
		update.SelDepth,
		update.Score,
		update.Time.Milliseconds(),
		update.Nodes,
		update.NPS,
		pvToString(update.PV),
	)
}

func (s *UCIServer) handleIsReady() {
	s.WriteString("readyok")
}

func (s *UCIServer) handleStop() {
	s.stopSearch(true)
}

func (s *UCIServer) stopSearch(wait bool) {
	cancel := s.currentCancel
	done := s.currentDone

	if cancel == nil {
		return
	}

	s.currentCancel = nil
	s.currentDone = nil

	cancel()
	if wait && done != nil {
		<-done
	}
}

func (s *UCIServer) parseGoOptions(args []string) goOptions {
	options := goOptions{depth: max(1, 64)}
	var (
		wtime, btime int
		winc, binc   int
		movestogo    int
		hasClock     bool
		hasDepth     bool
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "infinite":
			options.infinite = true
		case "depth":
			if i+1 < len(args) {
				if depth, err := strconv.Atoi(args[i+1]); err == nil && depth > 0 {
					options.depth = depth
					hasDepth = true
				}
				i++
			}
		case "movetime":
			if i+1 < len(args) {
				if ms, err := strconv.Atoi(args[i+1]); err == nil && ms > 0 {
					options.softLimit = time.Duration(ms) * time.Millisecond
					options.hardLimit = options.softLimit
				}
				i++
			}
		case "wtime":
			if i+1 < len(args) {
				if ms, err := strconv.Atoi(args[i+1]); err == nil {
					wtime = ms
					hasClock = true
				}
				i++
			}
		case "btime":
			if i+1 < len(args) {
				if ms, err := strconv.Atoi(args[i+1]); err == nil {
					btime = ms
					hasClock = true
				}
				i++
			}
		case "winc":
			if i+1 < len(args) {
				if ms, err := strconv.Atoi(args[i+1]); err == nil {
					winc = ms
				}
				i++
			}
		case "binc":
			if i+1 < len(args) {
				if ms, err := strconv.Atoi(args[i+1]); err == nil {
					binc = ms
				}
				i++
			}
		case "movestogo":
			if i+1 < len(args) {
				if moves, err := strconv.Atoi(args[i+1]); err == nil && moves > 0 {
					movestogo = moves
				}
				i++
			}
		}
	}

	if options.infinite {
		options.softLimit = 0
		options.hardLimit = 0
		return options
	}

	if options.hardLimit > 0 {
		return options
	}

	if hasClock {
		remaining := btime
		increment := binc
		if s.pos.Active() == chester.White {
			remaining = wtime
			increment = winc
		}

		remaining -= int(s.moveOverhead / time.Millisecond)
		if remaining < 1 {
			remaining = 1
		}

		if movestogo <= 0 {
			movestogo = 30
		}

		softMs := remaining/(movestogo+2) + increment/2
		if softMs < 10 {
			softMs = 10
		}

		hardMs := softMs + max(softMs/2, 10)
		if hardMs > remaining {
			hardMs = remaining
		}
		if softMs > hardMs {
			softMs = hardMs
		}

		options.softLimit = time.Duration(softMs) * time.Millisecond
		options.hardLimit = time.Duration(hardMs) * time.Millisecond
		return options
	}

	if !hasDepth {
		options.infinite = true
	}

	return options
}

func (s *UCIServer) handlePerft(args []string) {
	depth := 6

	if len(args) != 0 {
		if d, err := strconv.Atoi(args[0]); err == nil {
			depth = d
		} else {
			s.error("error parsing depth: %s", err)
			return
		}
	}

	go func() {
		nodes := 0
		start := time.Now()
		pos := *s.pos
		ch := chester.Perft(&pos, depth)
		for m := range ch {
			nodes += m.Count
			s.WriteString("%s: %d", m.Move, m.Count)
		}
		duration := time.Since(start)
		s.WriteString("perft %d in %s", nodes, duration)
	}()
}

func (s *UCIServer) handleCPUProfile(args []string) {
	if s.isCPUProfiling {
		s.info("cpu profiling stopped")
		pprof.StopCPUProfile()
		return
	}

	filename := "default.pgo"
	if len(args) != 0 {
		filename = args[0]
	}

	file, err := os.Create(filename)
	if err != nil {
		s.error("error creating profile file: %s", err)
		return
	}

	s.info("cpu profiling started")
	pprof.StartCPUProfile(file)
	s.CPUProfileFile = file
	s.isCPUProfiling = true
}

func (s *UCIServer) handleDebug(args []string) {
	if len(args) == 0 {
		s.isDebugLogging = !s.isDebugLogging
		return
	}

	switch args[0] {
	case "off":
		s.isDebugLogging = false
	case "on":
		s.isDebugLogging = true
	}
}

func (s *UCIServer) resetPosition() {
	pos, err := chester.ParseFEN(chester.DefaultFEN)
	if err != nil {
		s.error("error parsing fen: %s", err)
		return
	}
	s.pos = pos
}

func parseOption(args []string) (string, string) {
	nameIndex := -1
	valueIndex := -1
	for i, arg := range args {
		switch arg {
		case "name":
			nameIndex = i
		case "value":
			valueIndex = i
		}
	}

	if nameIndex == -1 {
		return "", ""
	}

	if valueIndex == -1 {
		return strings.Join(args[nameIndex+1:], " "), ""
	}

	return strings.Join(args[nameIndex+1:valueIndex], " "), strings.Join(args[valueIndex+1:], " ")
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func moveToUCI(move chester.Move) string {
	if move == 0 {
		return "0000"
	}
	return move.String()
}

func pvToString(pv []chester.Move) string {
	if len(pv) == 0 {
		return ""
	}

	var builder strings.Builder
	for i, move := range pv {
		if i > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(move.String())
	}
	return builder.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
