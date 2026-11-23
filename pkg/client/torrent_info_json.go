package client

import "encoding/json"

// UnmarshalJSON records whether qBittorrent supplied the inactive seeding
// limit. A partial torrent response can omit that field; treating the zero
// value as an explicit limit would otherwise replace qBittorrent's default
// global policy when another share-limit field is edited.
func (t *TorrentInfo) UnmarshalJSON(data []byte) error {
	type torrentInfoAlias TorrentInfo
	var decoded struct {
		*torrentInfoAlias
		Inactive *int64 `json:"inactive_seeding_time_limit"`
	}
	decoded.torrentInfoAlias = (*torrentInfoAlias)(t)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	// The pointer distinguishes a missing/null field from an explicit zero.
	// The embedded alias already received the numeric value when present.
	t.inactiveSeedingTLPresent = decoded.Inactive != nil
	if decoded.Inactive != nil {
		t.InactiveSeedingTL = *decoded.Inactive
	} else {
		// UnmarshalJSON may be called on a reused value. Do not let an older
		// response's limit masquerade as a value present in this response.
		t.InactiveSeedingTL = 0
	}
	return nil
}
