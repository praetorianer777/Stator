// Package collab relays a page's shared draft between the people editing it:
// Yjs updates and awareness in the framing of y-protocols, which the server
// stores and passes on without reading, and a few messages of Stator's own
// that say which life of the draft a browser is in and what it holds.
package collab

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Message types, the first varuint of every frame. Sync and awareness are
// y-protocols' own numbers; the rest are Stator's, and web/src/features/collab
// uses the same.
const (
	MsgSync           = 0
	MsgAwareness      = 1
	MsgQueryAwareness = 3
	// MsgRoom (server) names the room and its base and starts a load: the
	// updates that follow, up to an empty sync step 2, are all it holds.
	MsgRoom = 100
	// MsgPublished (browser) says it just published the given version from the room.
	MsgPublished = 101
	// MsgSeed (browser) is the room's first content, begun from a base version.
	MsgSeed = 102
	// MsgDiscard (browser) throws the room away for everybody.
	MsgDiscard = 103
	// MsgCompact (server) asks for the merge of the updates the last load carried.
	MsgCompact = 104
	// MsgCompacted (browser) is that merge.
	MsgCompacted = 105
	// MsgBase (server) says the room's base moved, after a publish from it.
	MsgBase = 107
)

// Sync message subtypes, as y-protocols numbers them.
const (
	SyncStep1  = 0
	SyncStep2  = 1
	SyncUpdate = 2
)

// emptyUpdate is a Yjs update that changes nothing: no structs, no deletions.
var emptyUpdate = []byte{0, 0}

var errMalformed = errors.New("malformed message")

// reader decodes lib0's encoding: unsigned varints of seven bits a byte, low
// bits first, and byte arrays and strings prefixed with their length.
type reader struct {
	buf []byte
	err error
}

func (r *reader) uint() uint64 {
	if r.err != nil {
		return 0
	}
	v, n := binary.Uvarint(r.buf)
	if n <= 0 {
		r.err = errMalformed
		return 0
	}
	r.buf = r.buf[n:]
	return v
}

func (r *reader) bytes() []byte {
	n := r.uint()
	if r.err != nil {
		return nil
	}
	if n > uint64(len(r.buf)) {
		r.err = errMalformed
		return nil
	}
	b := r.buf[:n]
	r.buf = r.buf[n:]
	return b
}

func (r *reader) string() string { return string(r.bytes()) }

// writer encodes the same.
type writer struct{ buf []byte }

func (w *writer) uint(v uint64) *writer {
	w.buf = binary.AppendUvarint(w.buf, v)
	return w
}

func (w *writer) bytes(b []byte) *writer {
	w.uint(uint64(len(b)))
	w.buf = append(w.buf, b...)
	return w
}

func (w *writer) string(s string) *writer { return w.bytes([]byte(s)) }

// Message is one decoded frame from a browser.
type Message struct {
	Type    uint64
	SubType uint64
	// Update is a sync update, a seed's content or a compaction's merge;
	// Awareness an awareness update.
	Update    []byte
	Awareness []byte
	Version   uint64
	From, To  uint64
	Count     uint64
}

// Decode reads a frame a browser sent.
func Decode(frame []byte) (Message, error) {
	r := &reader{buf: frame}
	m := Message{Type: r.uint()}
	switch m.Type {
	case MsgSync:
		m.SubType = r.uint()
		m.Update = r.bytes()
	case MsgAwareness:
		m.Awareness = r.bytes()
	case MsgQueryAwareness, MsgDiscard:
	case MsgPublished:
		m.Version = r.uint()
	case MsgSeed:
		m.Version = r.uint()
		m.Update = r.bytes()
	case MsgCompacted:
		m.From, m.To, m.Count = r.uint(), r.uint(), r.uint()
		m.Update = r.bytes()
	default:
		return m, fmt.Errorf("%w: type %d", errMalformed, m.Type)
	}
	if r.err != nil {
		return m, r.err
	}
	return m, nil
}

// UpdateFrame is a sync update to send on.
func UpdateFrame(update []byte) []byte {
	return (&writer{}).uint(MsgSync).uint(SyncUpdate).bytes(update).buf
}

// LoadedFrame ends a load: an empty sync step 2.
func LoadedFrame() []byte {
	return (&writer{}).uint(MsgSync).uint(SyncStep2).bytes(emptyUpdate).buf
}

// AwarenessFrame carries an awareness update.
func AwarenessFrame(update []byte) []byte {
	return (&writer{}).uint(MsgAwareness).bytes(update).buf
}

// RoomFrame names the room, its base, and whether the receiver is to seed it.
func RoomFrame(room uuid.UUID, base int, seed bool) []byte {
	s := uint64(0)
	if seed {
		s = 1
	}
	return (&writer{}).uint(MsgRoom).string(room.String()).uint(uint64(base)).uint(s).buf
}

// BaseFrame says the room's base moved.
func BaseFrame(base int) []byte {
	return (&writer{}).uint(MsgBase).uint(uint64(base)).buf
}

// CompactFrame asks for the merge of the count updates numbered from through to.
func CompactFrame(from, to int64, count int) []byte {
	return (&writer{}).uint(MsgCompact).uint(uint64(from)).uint(uint64(to)).uint(uint64(count)).buf
}
