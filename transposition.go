package chester

import "unsafe"

type ttEntry struct {
	key      uint64
	bestMove Move
	score    int
	depth    int16
	flag     uint8
}

type TranspositionTable struct {
	entries []ttEntry
	mask    uint64
}

func NewTranspositionTable(hashMB int) *TranspositionTable {
	tt := &TranspositionTable{}
	tt.Resize(hashMB)
	return tt
}

func (tt *TranspositionTable) Resize(hashMB int) {
	if hashMB <= 0 {
		hashMB = 1
	}

	targetBytes := hashMB * 1024 * 1024
	entrySize := int(unsafe.Sizeof(ttEntry{}))
	targetEntries := targetBytes / entrySize
	if targetEntries < 1 {
		targetEntries = 1
	}

	size := 1
	for size*2 <= targetEntries {
		size *= 2
	}

	tt.entries = make([]ttEntry, size)
	tt.mask = uint64(size - 1)
}

func (tt *TranspositionTable) Clear() {
	for i := range tt.entries {
		tt.entries[i] = ttEntry{}
	}
}

func (tt *TranspositionTable) lookup(key uint64) (ttEntry, bool) {
	if tt == nil || len(tt.entries) == 0 {
		return ttEntry{}, false
	}

	entry := tt.entries[key&tt.mask]
	if entry.key != key {
		return ttEntry{}, false
	}

	return entry, true
}

func (tt *TranspositionTable) store(entry ttEntry) {
	if tt == nil || len(tt.entries) == 0 {
		return
	}

	slot := &tt.entries[entry.key&tt.mask]
	if slot.key != 0 && slot.key != entry.key && slot.depth > entry.depth {
		return
	}

	*slot = entry
}
