package collab

// Peer is one browser's awareness: its clock and its state, the JSON text
// y-protocols sends, "null" once it has gone.
type Peer struct {
	Client uint64
	Clock  uint64
	State  string
}

// gone is the state y-protocols sends for a browser that left.
const gone = "null"

// MaxPeersPerUpdate bounds how many browsers one awareness update may speak
// for; a browser speaks for itself, so a few is plenty.
const MaxPeersPerUpdate = 16

// ParseAwareness reads an awareness update: how many browsers, then each
// one's client id, clock and state.
func ParseAwareness(update []byte) ([]Peer, error) {
	r := &reader{buf: update}
	n := r.uint()
	if r.err == nil && n > MaxPeersPerUpdate {
		return nil, errMalformed
	}
	peers := make([]Peer, 0, n)
	for i := uint64(0); i < n && r.err == nil; i++ {
		peers = append(peers, Peer{Client: r.uint(), Clock: r.uint(), State: r.string()})
	}
	if r.err != nil {
		return nil, r.err
	}
	return peers, nil
}

// EncodeAwareness writes peers as one awareness update.
func EncodeAwareness(peers []Peer) []byte {
	w := (&writer{}).uint(uint64(len(peers)))
	for _, p := range peers {
		w.uint(p.Client).uint(p.Clock).string(p.State)
	}
	return w.buf
}
