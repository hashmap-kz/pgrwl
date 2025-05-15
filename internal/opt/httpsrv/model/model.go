package model

type WALArchiveSize struct {
	Bytes int64  `json:"bytes,omitempty"`
	IEC   string `json:"iec,omitempty"`
}
