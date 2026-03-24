# Chester

<p align="center">
  <img src="logo/logo.png" width="500"/>
</p>

[![Go Reference](https://pkg.go.dev/badge/github.com/bluescreen10/chester.svg)](https://pkg.go.dev/github.com/bluescreen10/chester)
[![Tests](https://github.com/bluescreen10/chester/actions/workflows/go.yml/badge.svg)](https://github.com/bluescreen10/chester/actions)
[![codecov](https://codecov.io/gh/bluescreen10/chester/branch/main/graph/badge.svg)](https://codecov.io/gh/bluescreen10/chester)
[![Go Report Card](https://goreportcard.com/badge/github.com/bluescreen10/chester)](https://goreportcard.com/report/github.com/bluescreen10/chester)
![Go Version](https://img.shields.io/github/go-mod/go-version/bluescreen10/chester)

A chess library and engine written in Go.

## Library Features

- Fast legal move generation using bitboards
- FEN (Forsyth–Edwards Notation) parsing and serialization
- Magic bitboard sliding piece attack lookup
- Zobrist hashing (Polyglot-compatible)
- Perft for move generation testing and benchmarking

## Engine Features

- Universal Chess Interface (UCI)
- Negamax with Alpha-Beta pruning
- Quiescence search
- PeSTO evaluation function
- Opening book support (Polyglot `.bin` format)

## Installation

```bash
go get github.com/bluescreen10/chester
```

## Usage

### As a library

```go
import "github.com/bluescreen10/chester"

// Parse a position from FEN
pos, err := chester.ParseFEN(chester.DefaultFEN)
if err != nil {
    panic(err)
}

// Generate legal moves
var moves []chester.Move
moves, inCheck := chester.LegalMoves(moves, &pos)

// Apply a move
pos.Do(moves[0])

// Serialize back to FEN
fmt.Println(pos.FEN())

// Perft
results := chester.Perft(&pos, 6)
var nodes int
for r := range results {
    nodes += r.Count
}
fmt.Printf("nodes: %d\n", nodes)
```

### As an engine (UCI)

```bash
go run ./cmd
```

```
position startpos
perft 6

a2a3: 4463267
b2b3: 5310358
c2c3: 5417640
d2d3: 8073082
e2e3: 9726018
f2f3: 4404141
g2g3: 5346260
h2h3: 4463070
a2a4: 5363555
b2b4: 5293555
c2c4: 5866666
d2d4: 8879566
e2e4: 9771632
f2f4: 4890429
g2g4: 5239875
h2h4: 5385554
b1a3: 4856835
b1c3: 5708064
g1f3: 5723523
g1h3: 4877234
perft 119060324 in 363.214708ms
```

(The above results were produced in a Macbook Pro M2)

## References
- [Chess Programming Wiki](https://www.chessprogramming.org)
- [Magic Bitboards](https://www.chessprogramming.org/Magic_Bitboards)
- [Polyglot Opening Book Format](http://hgm.nubati.net/book_format.html)

## Chester Gauntlet

CCRL Blitz gauntlet results for `Chester 0.1.0` against fixed-opponent ratings:

| Name | Rating | Score |
| --- | ---: | ---: |
| Goldfish 2.1.1 | 2254 | 5/40 |
| Halcyon 1 | 2102 | 7.5/40 |
| SOS 5.1 | 2543 | 9/40 |
| GearHeart 0.16 TP | 1703 | 22.5/40 |
| GC Engine 1.2 | 965 | 30/40 |
| SebLague Chess Engine | 1368 | 30.5/40 |
| Dragontooth 0.2 | 1041 | 32/40 |
| DoctorB 1.21 | 1327 | 33.5/40 |
| Performance Rating | 1685 | 170/320 |
