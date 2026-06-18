package jd

import "time"

// Requirement is one discrete hiring need extracted from a job description.
type Requirement struct {
	Text     string `json:"text"`
	MustHave bool   `json:"must_have"`
}

// Posting is the cached result of acquiring one job description. It is keyed on
// disk by sha256(URL) so the same JD is never re-fetched or re-processed.
type Posting struct {
	URL          string        `json:"url"`
	FetchedAt    time.Time     `json:"fetched_at"`
	RawText      string        `json:"raw_text"`
	Requirements []Requirement `json:"requirements"`
}
