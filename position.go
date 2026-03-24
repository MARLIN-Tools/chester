package chester

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultFEN is the FEN string for the standard starting position.
const DefaultFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

// Color represents which side owns a set of pieces.
type Color uint8

const (
	White Color = iota
	Black
)

// Piece identifies the type of a chess piece.
// Empty is a sentinel value meaning no piece occupies a square.
type Piece uint8

const (
	Pawn = Piece(iota)
	Knight
	Bishop
	Rook
	Queen
	King
	Empty
)

// castlingRights is a bitmask encoding which of the four castling options
// remain available. Each bit corresponds to one option.
type castlingRights uint8

const (
	whiteKingSideCastle castlingRights = 1 << iota
	whiteQueenSideCastle
	blackKingSideCastle
	blackQueenSideCastle
)

// Position holds the complete state of a chess position. It combines a
// bitboard representation (one Bitboard per piece type, one per color) with
// a mailbox for O(1) square queries and an incrementally maintained
// Polyglot-compatible Zobrist hash.
//
// The zero value is not a valid position; use ParseFEN to construct one.
type Position struct {
	// One bitboard per piece type, shared across colors.
	pieces [Piece(6)]Bitboard

	// Combined occupancy per color: [White] and [Black].
	allPieces [Color(2)]Bitboard

	// Zobrist hash maintained incrementally by Do, move, put, and remove.
	hash uint64

	// Maps each square index to the piece occupying it, or Empty.
	mailbox [64]Piece

	// Square a pawn that just double-pushed now occupies;
	// SQ_NULL when en passant is unavailable.
	enPassantTarget Square

	// Bitmask of currently available castling options.
	castlingRights castlingRights

	// Side to move and side waiting.
	active, inactive Color

	// Half-move clock for the fifty-move rule; resets on pawn move or capture.
	halfMoves uint8

	// Full-move counter; starts at 1 and increments after Black's move.
	fullMoves uint16
}

// ParseFEN parses a FEN string and returns a pointer to the resulting Position.
// All six FEN fields are required: piece placement, active color, castling
// availability, en passant target, half-move clock, and full-move number.
// The Zobrist hash is computed from scratch before returning.
// Returns an error if the string is malformed or contains an unrecognised character.
func ParseFEN(fen string) (*Position, error) {
	var pos Position

	parts := strings.Split(strings.TrimSpace(fen), " ")
	if len(parts) < 6 {
		return &pos, fmt.Errorf("invalid fen: %s", fen)
	}

	for i := range 64 {
		pos.mailbox[i] = Empty
	}

	bit := Bitboard(1)
	sq := Square(0)

	for _, row := range strings.Split(parts[0], "/") {
		for _, char := range row {
			switch char {
			case 'P':
				pos.mailbox[sq] = Pawn
				pos.pieces[Pawn] |= bit
				pos.allPieces[White] |= bit
			case 'N':
				pos.mailbox[sq] = Knight
				pos.pieces[Knight] |= bit
				pos.allPieces[White] |= bit
			case 'B':
				pos.mailbox[sq] = Bishop
				pos.pieces[Bishop] |= bit
				pos.allPieces[White] |= bit
			case 'R':
				pos.mailbox[sq] = Rook
				pos.pieces[Rook] |= bit
				pos.allPieces[White] |= bit
			case 'Q':
				pos.mailbox[sq] = Queen
				pos.pieces[Queen] |= bit
				pos.allPieces[White] |= bit
			case 'K':
				pos.mailbox[sq] = King
				pos.pieces[King] |= bit
				pos.allPieces[White] |= bit
			case 'p':
				pos.mailbox[sq] = Pawn
				pos.pieces[Pawn] |= bit
				pos.allPieces[Black] |= bit
			case 'n':
				pos.mailbox[sq] = Knight
				pos.pieces[Knight] |= bit
				pos.allPieces[Black] |= bit
			case 'b':
				pos.mailbox[sq] = Bishop
				pos.pieces[Bishop] |= bit
				pos.allPieces[Black] |= bit
			case 'r':
				pos.mailbox[sq] = Rook
				pos.pieces[Rook] |= bit
				pos.allPieces[Black] |= bit
			case 'q':
				pos.mailbox[sq] = Queen
				pos.pieces[Queen] |= bit
				pos.allPieces[Black] |= bit
			case 'k':
				pos.mailbox[sq] = King
				pos.pieces[King] |= bit
				pos.allPieces[Black] |= bit
			case '1', '2', '3', '4', '5', '6', '7', '8':
				bit <<= uint(char - '1')
				sq += Square(char - '1')
			default:
				return &Position{}, fmt.Errorf("invalid piece: %c", char)
			}
			bit <<= 1
			sq++
		}
	}

	if parts[1] == "w" || parts[1] == "W" {
		pos.active = White
		pos.inactive = Black
	} else {
		pos.active = Black
		pos.inactive = White
	}

	for _, c := range parts[2] {
		switch c {
		case 'K':
			pos.castlingRights |= whiteKingSideCastle
		case 'Q':
			pos.castlingRights |= whiteQueenSideCastle
		case 'k':
			pos.castlingRights |= blackKingSideCastle
		case 'q':
			pos.castlingRights |= blackQueenSideCastle
		}
	}

	pos.enPassantTarget = SQ_NULL

	if parts[3] != "-" {
		enPassantTarget, err := ParseSquare(parts[3])
		if err != nil {
			return &pos, fmt.Errorf("invalid en passant square: %s", fen)
		}
		pos.enPassantTarget = enPassantTarget
	}

	halfMoves, err := strconv.Atoi(parts[4])
	if err != nil {
		return &pos, fmt.Errorf("invalid half moves: %s", fen)
	}
	pos.halfMoves = uint8(halfMoves)

	fullMoves, err := strconv.Atoi(parts[5])
	if err != nil {
		return &pos, fmt.Errorf("invalid full moves: %s", fen)
	}
	pos.fullMoves = uint16(fullMoves)

	pos.hash = computeHash(&pos)
	return &pos, nil
}

// FEN returns the FEN string representation of the position.
func (p *Position) FEN() string {
	fen := strings.Builder{}

	for bit := Bitboard(1); bit != 0; bit <<= 1 {
		if bit&File_A != 0 && bit > 1 {
			fen.WriteByte('/')
		}

		if p.allPieces[White]&p.pieces[Pawn]&bit != 0 {
			fen.WriteByte('P')
		} else if p.allPieces[Black]&p.pieces[Pawn]&bit != 0 {
			fen.WriteByte('p')
		} else if p.allPieces[White]&p.pieces[Knight]&bit != 0 {
			fen.WriteByte('N')
		} else if p.allPieces[Black]&p.pieces[Knight]&bit != 0 {
			fen.WriteByte('n')
		} else if p.allPieces[White]&p.pieces[Bishop]&bit != 0 {
			fen.WriteByte('B')
		} else if p.allPieces[Black]&p.pieces[Bishop]&bit != 0 {
			fen.WriteByte('b')
		} else if p.allPieces[White]&p.pieces[Rook]&bit != 0 {
			fen.WriteByte('R')
		} else if p.allPieces[Black]&p.pieces[Rook]&bit != 0 {
			fen.WriteByte('r')
		} else if p.allPieces[White]&p.pieces[Queen]&bit != 0 {
			fen.WriteByte('Q')
		} else if p.allPieces[Black]&p.pieces[Queen]&bit != 0 {
			fen.WriteByte('q')
		} else if p.allPieces[White]&p.pieces[King]&bit != 0 {
			fen.WriteByte('K')
		} else if p.allPieces[Black]&p.pieces[King]&bit != 0 {
			fen.WriteByte('k')
		} else {
			empty := 1
			bit <<= 1
			for ; bit != 0; bit, empty = bit<<1, empty+1 {
				if (p.allPieces[White]|p.allPieces[Black])&bit != 0 || bit&File_A != 0 {
					bit >>= 1
					break
				}
			}

			fen.WriteByte('0' + byte(empty))
		}
	}

	fen.WriteByte(' ')

	if p.Active() == White {
		fen.WriteByte('w')
	} else {
		fen.WriteByte('b')
	}

	fen.WriteByte(' ')

	if p.CanWhiteCastleKingSide() || p.CanWhiteCastleQueenSide() || p.CanBlackCastleKingSide() || p.CanBlackCastleQueenSide() {
		if p.CanWhiteCastleKingSide() {
			fen.WriteByte('K')
		}

		if p.CanWhiteCastleQueenSide() {
			fen.WriteByte('Q')
		}

		if p.CanBlackCastleKingSide() {
			fen.WriteByte('k')
		}

		if p.CanBlackCastleQueenSide() {
			fen.WriteByte('q')
		}
	} else {
		fen.WriteString("-")
	}

	fen.WriteByte(' ')
	fen.WriteString(p.enPassantTarget.String())
	fen.WriteString(fmt.Sprintf(" %d %d", p.halfMoves, p.fullMoves))

	return fen.String()
}

// String returns a human-readable ASCII board diagram with rank numbers and
// file letters. White pieces are uppercase, black pieces are lowercase.
func (p *Position) String() string {
	builder := strings.Builder{}

	builder.WriteString("+---+---+---+---+---+---+---+---+\n")
	bit := Bitboard(1)
	for rank := 7; rank >= 0; rank-- {
		builder.WriteByte('|')
		for file := 0; file < 8; file++ {
			if p.allPieces[White]&p.pieces[Pawn]&bit != 0 {
				builder.WriteString(" P |")
			} else if p.allPieces[Black]&p.pieces[Pawn]&bit != 0 {
				builder.WriteString(" p |")
			} else if p.allPieces[White]&p.pieces[Knight]&bit != 0 {
				builder.WriteString(" N |")
			} else if p.allPieces[Black]&p.pieces[Knight]&bit != 0 {
				builder.WriteString(" n |")
			} else if p.allPieces[White]&p.pieces[Bishop]&bit != 0 {
				builder.WriteString(" B |")
			} else if p.allPieces[Black]&p.pieces[Bishop]&bit != 0 {
				builder.WriteString(" b |")
			} else if p.allPieces[White]&p.pieces[Rook]&bit != 0 {
				builder.WriteString(" R |")
			} else if p.allPieces[Black]&p.pieces[Rook]&bit != 0 {
				builder.WriteString(" r |")
			} else if p.allPieces[White]&p.pieces[Queen]&bit != 0 {
				builder.WriteString(" Q |")
			} else if p.allPieces[Black]&p.pieces[Queen]&bit != 0 {
				builder.WriteString(" q |")
			} else if p.allPieces[White]&p.pieces[King]&bit != 0 {
				builder.WriteString(" K |")
			} else if p.allPieces[Black]&p.pieces[King]&bit != 0 {
				builder.WriteString(" k |")
			} else {
				builder.WriteString("   |")
			}
			bit <<= 1
		}
		builder.WriteString(fmt.Sprintf(" %d\n", rank+1))
		builder.WriteString("+---+---+---+---+---+---+---+---+\n")
	}
	builder.WriteString("  a   b   c   d   e   f   g   h\n")
	return builder.String()
}

// Active returns the color of the side whose turn it is to move.
func (p *Position) Active() Color {
	return p.active
}

// Inactive returns the color of the side that is waiting to move.
func (p *Position) Inactive() Color {
	return p.inactive
}

// CanWhiteCastleKingSide reports whether white retains the right to castle
// kingside. It does not verify that the path is clear or unattacked.
func (p *Position) CanWhiteCastleKingSide() bool {
	return p.castlingRights&whiteKingSideCastle != 0
}

// CanWhiteCastleQueenSide reports whether white retains the right to castle
// queenside. It does not verify that the path is clear or unattacked.
func (p *Position) CanWhiteCastleQueenSide() bool {
	return p.castlingRights&whiteQueenSideCastle != 0
}

// CanBlackCastleKingSide reports whether black retains the right to castle
// kingside. It does not verify that the path is clear or unattacked.
func (p *Position) CanBlackCastleKingSide() bool {
	return p.castlingRights&blackKingSideCastle != 0
}

// CanBlackCastleQueenSide reports whether black retains the right to castle
// queenside. It does not verify that the path is clear or unattacked.
func (p *Position) CanBlackCastleQueenSide() bool {
	return p.castlingRights&blackQueenSideCastle != 0
}

// Occupied returns a Bitboard with a bit set for every square occupied by
// either color.
func (p *Position) Occupied() Bitboard {
	return p.allPieces[White] | p.allPieces[Black]
}

// Do applies a move to the position, updating piece placement, the mailbox,
// castling rights, en passant state, half-move clock, full-move counter,
// active/inactive colors, and the Zobrist hash. The move must be legal;
// Do does not validate it.
func (p *Position) Do(m Move) {
	from := m.From()
	to := m.To()

	enPassantTarget := p.enPassantTarget
	p.enPassantTarget = SQ_NULL
	p.halfMoves++

	piece := p.mailbox[from]
	captured := p.mailbox[to]
	isCapture := captured != Empty
	diff := int(to) - int(from)

	if enPassantTarget != SQ_NULL {
		var pawnSq Square
		if p.active == White {
			pawnSq = enPassantTarget + 8 // black pawn moved to higher index
		} else {
			pawnSq = enPassantTarget - 8 // white pawn moved to lower index
		}
		if p.adjacentPawns(p.active, pawnSq) {
			p.hash ^= polyglotTable.EnPassant[enPassantTarget.File()]
		}
	}

	if isCapture {
		p.halfMoves = 0
		p.remove(captured, p.inactive, to)
		if captured == Rook {
			p.updateCastlingRights(to)
		}
	}

	// if enPassantTarget != SQ_NULL {
	// 	p.hash ^= polyglotTable.EnPassant[enPassantTarget.File()]
	// }

	switch piece {
	case Pawn:
		p.halfMoves = 0
		switch {
		case m.IsPromotion():
			p.remove(Pawn, p.active, from)
			p.put(m.PromoPiece(), p.active, to)
		case to == enPassantTarget:
			p.move(Pawn, p.active, from, to)
			if p.active == White {
				p.remove(Pawn, p.inactive, to+8)
			} else {
				p.remove(Pawn, p.inactive, to-8)
			}
		case diff == 16 || diff == -16:
			p.move(Pawn, p.active, from, to)
			p.enPassantTarget = (to + from) >> 1
			if p.adjacentPawns(p.inactive, to) {
				p.hash ^= polyglotTable.EnPassant[p.enPassantTarget.File()]
			}
		default:
			p.move(Pawn, p.active, from, to)
		}
	case King:
		p.move(King, p.active, from, to)
		p.updateCastlingRights(from)

		switch diff {
		case 2:
			if p.active == White {
				p.move(Rook, White, SQ_H1, SQ_F1)
			} else {
				p.move(Rook, Black, SQ_H8, SQ_F8)
			}
		case -2:
			if p.active == White {
				p.move(Rook, White, SQ_A1, SQ_D1)
			} else {
				p.move(Rook, Black, SQ_A8, SQ_D8)
			}
		}

	default:
		p.move(piece, p.active, from, to)
		if piece == Rook {
			p.updateCastlingRights(from)
		}
	}

	p.hash ^= polyglotTable.WhiteToMove
	p.fullMoves += uint16(p.active)
	p.active, p.inactive = p.inactive, p.active
}

// Get returns the piece occupying sq, or Empty if the square is unoccupied.
func (p *Position) Get(sq Square) Piece {
	return p.mailbox[sq]
}

// Pawns returns a Bitboard of all pawns belonging to the active color.
func (p *Position) Pawns() Bitboard {
	return p.pieces[Pawn] & p.allPieces[p.active]
}

// WhitePawns returns a Bitboard of all white pawns.
func (p *Position) WhitePawns() Bitboard {
	return p.pieces[Pawn] & p.allPieces[White]
}

// BlackPawns returns a Bitboard of all black pawns.
func (p *Position) BlackPawns() Bitboard {
	return p.pieces[Pawn] & p.allPieces[Black]
}

// EnemyPawns returns a Bitboard of all pawns belonging to the inactive color.
func (p *Position) EnemyPawns() Bitboard {
	return p.pieces[Pawn] & p.allPieces[p.inactive]
}

// Knights returns a Bitboard of all knights belonging to the active color.
func (p *Position) Knights() Bitboard {
	return p.pieces[Knight] & p.allPieces[p.active]
}

// WhiteKnights returns a Bitboard of all white knights.
func (p *Position) WhiteKnights() Bitboard {
	return p.pieces[Knight] & p.allPieces[White]
}

// BlackKnights returns a Bitboard of all black knights.
func (p *Position) BlackKnights() Bitboard {
	return p.pieces[Knight] & p.allPieces[Black]
}

// EnemyKnights returns a Bitboard of all knights belonging to the inactive color.
func (p *Position) EnemyKnights() Bitboard {
	return p.pieces[Knight] & p.allPieces[p.inactive]
}

// Bishops returns a Bitboard of all bishops belonging to the active color.
func (p *Position) Bishops() Bitboard {
	return p.pieces[Bishop] & p.allPieces[p.active]
}

// WhiteBishops returns a Bitboard of all white bishops.
func (p *Position) WhiteBishops() Bitboard {
	return p.pieces[Bishop] & p.allPieces[White]
}

// BlackBishops returns a Bitboard of all black bishops.
func (p *Position) BlackBishops() Bitboard {
	return p.pieces[Bishop] & p.allPieces[Black]
}

// EnemyBishops returns a Bitboard of all bishops belonging to the inactive color.
func (p *Position) EnemyBishops() Bitboard {
	return p.pieces[Bishop] & p.allPieces[p.inactive]
}

// Rooks returns a Bitboard of all rooks belonging to the active color.
func (p *Position) Rooks() Bitboard {
	return p.pieces[Rook] & p.allPieces[p.active]
}

// WhiteRooks returns a Bitboard of all white rooks.
func (p *Position) WhiteRooks() Bitboard {
	return p.pieces[Rook] & p.allPieces[White]
}

// BlackRooks returns a Bitboard of all black rooks.
func (p *Position) BlackRooks() Bitboard {
	return p.pieces[Rook] & p.allPieces[Black]
}

// EnemyRooks returns a Bitboard of all rooks belonging to the inactive color.
func (p *Position) EnemyRooks() Bitboard {
	return p.pieces[Rook] & p.allPieces[p.inactive]
}

// Queens returns a Bitboard of all queens belonging to the active color.
func (p *Position) Queens() Bitboard {
	return p.pieces[Queen] & p.allPieces[p.active]
}

// WhiteQueens returns a Bitboard of all white queens.
func (p *Position) WhiteQueens() Bitboard {
	return p.pieces[Queen] & p.allPieces[White]
}

// BlackQueens returns a Bitboard of all black queens.
func (p *Position) BlackQueens() Bitboard {
	return p.pieces[Queen] & p.allPieces[Black]
}

// EnemyQueens returns a Bitboard of all queens belonging to the inactive color.
func (p *Position) EnemyQueens() Bitboard {
	return p.pieces[Queen] & p.allPieces[p.inactive]
}

// King returns a Bitboard with the bit set for the active color's king square.
func (p *Position) King() Bitboard {
	return p.pieces[King] & p.allPieces[p.active]
}

// WhiteKing returns a Bitboard with the bit set for the white king's square.
func (p *Position) WhiteKing() Bitboard {
	return p.pieces[King] & p.allPieces[White]
}

// BlackKing returns a Bitboard with the bit set for the black king's square.
func (p *Position) BlackKing() Bitboard {
	return p.pieces[King] & p.allPieces[Black]
}

// EnemyKing returns a Bitboard with the bit set for the inactive color's king square.
func (p *Position) EnemyKing() Bitboard {
	return p.pieces[King] & p.allPieces[p.inactive]
}

// EnemyQueensOrBishops returns a Bitboard of all enemy queens and bishops —
// the pieces that generate diagonal threats.
func (p *Position) EnemyQueensOrBishops() Bitboard {
	return (p.pieces[Queen] | p.pieces[Bishop]) & p.allPieces[p.inactive]
}

// EnemyQueensOrRooks returns a Bitboard of all enemy queens and rooks —
// the pieces that generate straight (rank/file) threats.
func (p *Position) EnemyQueensOrRooks() Bitboard {
	return (p.pieces[Queen] | p.pieces[Rook]) & p.allPieces[p.inactive]
}

// Enemies returns a Bitboard of squares occupied by capturable inactive-color
// pieces. The enemy king is excluded because legal moves may not capture it.
func (p *Position) Enemies() Bitboard {
	return p.allPieces[p.inactive] &^ p.EnemyKing()
}

// EnemiesOrEmpty returns a Bitboard of all squares that are either empty or
// occupied by the inactive color — i.e. valid landing squares for active-color
// pieces when excluding friendly captures. The enemy king square is excluded,
// because legal moves may not move onto it.
func (p *Position) EnemiesOrEmpty() Bitboard {
	return ^p.allPieces[p.active] &^ p.EnemyKing()
}

// FullMoves returns the full-move counter. It starts at 1 and is incremented
// after Black's move, matching FEN semantics.
func (p *Position) FullMoves() uint16 {
	return p.fullMoves
}

// HalfMoves returns the half-move clock used for the fifty-move rule.
// It resets to zero on any pawn move or capture.
func (p *Position) HalfMoves() uint8 {
	return p.halfMoves
}

// EnPassantTarget returns the square of the pawn eligible to be captured via
// en passant, or SQ_NULL when en passant is unavailable.
func (p *Position) EnPassantTarget() Square {
	return p.enPassantTarget
}

// Hash returns the Polyglot-compatible Zobrist hash of the position.
// The hash is maintained incrementally by Do and will always match
// computeHash for a correctly constructed position.
func (p *Position) Hash() uint64 {
	return p.hash
}

func (p *Position) WhitePieces() Bitboard {
	return p.allPieces[White]
}

func (p *Position) BlackPieces() Bitboard {
	return p.allPieces[Black]
}

// adjacentPawns reports whether any pawn of the given color occupies a square
// horizontally adjacent to sq. Used to determine whether the en passant file
// should be included in the Zobrist hash.
func (p *Position) adjacentPawns(color Color, sq Square) bool {
	pawns := p.pieces[Pawn] & p.allPieces[color]
	bb := NewBitboardFromSquare(sq)
	return bb&((pawns&File_Not_H)<<1|(pawns&File_Not_A)>>1) != 0
}

// updateCastlingRights revokes any castling rights associated with sq (a rook
// origin or king origin square) and updates the Zobrist hash accordingly.
// It XORs out the old castling hash component before modifying the rights and
// XORs in the new one after, so the hash remains consistent.
func (p *Position) updateCastlingRights(sq Square) {
	fromTo := NewBitboardFromSquare(sq)
	p.hash ^= polyglotTable.Castling[p.castlingRights]
	p.castlingRights &^= whiteKingSideCastle * castlingRights((fromTo&BB_SQ_H1)>>SQ_H1)
	p.castlingRights &^= (whiteKingSideCastle | whiteQueenSideCastle) * castlingRights((fromTo&BB_SQ_E1)>>SQ_E1)
	p.castlingRights &^= whiteQueenSideCastle * castlingRights((fromTo&BB_SQ_A1)>>SQ_A1)

	p.castlingRights &^= blackKingSideCastle * castlingRights((fromTo&BB_SQ_H8)>>SQ_H8)
	p.castlingRights &^= (blackKingSideCastle | blackQueenSideCastle) * castlingRights((fromTo&BB_SQ_E8)>>SQ_E8)
	p.castlingRights &^= blackQueenSideCastle * castlingRights((fromTo&BB_SQ_A8)>>SQ_A8)
	p.hash ^= polyglotTable.Castling[p.castlingRights]
}

// move relocates piece of color from from to to, updating the mailbox,
// bitboards, and Zobrist hash.
func (p *Position) move(piece Piece, color Color, from, to Square) {
	p.mailbox[from] = Empty
	p.mailbox[to] = piece
	fromAndTo := NewBitboardFromSquare(from) | NewBitboardFromSquare(to)
	p.allPieces[color] ^= fromAndTo
	p.pieces[piece] ^= fromAndTo
	p.hash ^= polyglotTable.Pieces[color][piece][from]
	p.hash ^= polyglotTable.Pieces[color][piece][to]
}

// put places piece of color on sq, updating the mailbox, bitboards,
// and Zobrist hash. Used for promotion placement.
func (p *Position) put(piece Piece, color Color, sq Square) {
	p.mailbox[sq] = piece

	bb := NewBitboardFromSquare(sq)
	p.allPieces[color] |= bb
	p.pieces[piece] |= bb
	p.hash ^= polyglotTable.Pieces[color][piece][sq]
}

// remove clears piece of color from sq, updating the mailbox, bitboards,
// and Zobrist hash. Used for captures and promotion removal.
func (p *Position) remove(piece Piece, color Color, sq Square) {
	p.mailbox[sq] = Empty

	bb := NewBitboardFromSquare(sq)
	p.allPieces[color] &^= bb
	p.pieces[piece] &^= bb
	p.hash ^= polyglotTable.Pieces[color][piece][sq]
}

// computeHash rebuilds the Polyglot-compatible Zobrist hash of p from scratch.
// It is called once by ParseFEN; thereafter the hash is maintained
// incrementally.
func computeHash(p *Position) uint64 {
	var hash uint64

	for color := range Color(2) {
		for piece, bb := range p.pieces {
			bb = bb & p.allPieces[color]
			var sq Square
			for bb != 0 {
				sq, bb = bb.PopLSB()
				hash ^= polyglotTable.Pieces[color][piece][sq]
			}
		}
	}

	hash ^= polyglotTable.Castling[p.castlingRights]

	if p.enPassantTarget != SQ_NULL && p.adjacentPawns(p.active, p.enPassantTarget) {
		file := p.enPassantTarget.File()
		hash ^= polyglotTable.EnPassant[file]
	}

	if p.Active() == White {
		hash ^= polyglotTable.WhiteToMove
	}

	return hash
}
