package model

// JSON shapes of the moderation / administration API (no web UI consumes them yet; see docs/api-contract.md).

type AdminReport struct {
	ID            string  `json:"id"`
	TargetType    string  `json:"targetType"` // video | comment | user
	TargetID      string  `json:"targetId"`
	Reason        string  `json:"reason"`
	Description   string  `json:"description"`
	Status        string  `json:"status"`   // pending | under_review | resolved | rejected
	Priority      int     `json:"priority"` // 0 urgent, 1 normal, 2 low
	AssignedTo    *string `json:"assignedTo"`
	CreatedAt     string  `json:"createdAt"`
	Reporter      *string `json:"reporter"`
	OpenForTarget int     `json:"openForTarget"` // open reports about the same target
	Preview       string  `json:"preview"`       // video title / comment text / username
}

type AdminUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	IsCreator   bool   `json:"isCreator"`
	CreatedAt   string `json:"createdAt"`
}

type AuditEntry struct {
	ID         string         `json:"id"`
	ActorID    *string        `json:"actorId"`
	Actor      *string        `json:"actor"`
	Action     string         `json:"action"`
	TargetType *string        `json:"targetType"`
	TargetID   *string        `json:"targetId"`
	Metadata   map[string]any `json:"metadata"`
	CreatedAt  string         `json:"createdAt"`
}

type BlockedHash struct {
	SHA256    string `json:"sha256"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"createdAt"`
}

type HashBlocked struct {
	VideosRemoved int64 `json:"videosRemoved"`
}
