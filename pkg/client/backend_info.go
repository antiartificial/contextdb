package client

// BackendInfo describes the effective stores without exposing connection secrets.
type BackendInfo struct {
	Mode       Mode   `json:"mode"`
	Graph      string `json:"graph"`
	Vector     string `json:"vector"`
	KV         string `json:"kv"`
	Events     string `json:"events"`
	Persistent bool   `json:"persistent"`
}

// Backends returns the actual storage configuration selected by Open.
func (db *DB) Backends() BackendInfo {
	switch db.opts.Mode {
	case ModeStandard:
		return BackendInfo{Mode: ModeStandard, Graph: "postgres", Vector: "pgvector", KV: "postgres", Events: "postgres", Persistent: true}
	case ModeRemote:
		return BackendInfo{Mode: ModeRemote, Graph: "remote", Vector: "remote", KV: "remote", Events: "remote"}
	default:
		if db.opts.DataDir != "" {
			return BackendInfo{Mode: ModeEmbedded, Graph: "badger", Vector: "hnsw", KV: "badger", Events: "badger", Persistent: true}
		}
		return BackendInfo{Mode: ModeEmbedded, Graph: "memory", Vector: "memory", KV: "memory", Events: "memory"}
	}
}
