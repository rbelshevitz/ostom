package storage

// Space describes the capacity of the volume containing a recovery output.
// Values are advisory: actual export size depends on message and attachment
// content, not only on the source container size.
type Space struct {
	Available uint64 `json:"available_bytes"`
	Total     uint64 `json:"total_bytes"`
	Free      uint64 `json:"free_bytes"`
}
