package evidence

import (
	"encoding/json"
	"strings"
)

// Known framework pack ids validated at evidence record time (deploy may enforce stricter rules).
var knownFrameworkPacks = map[string]struct{}{
	"eu-ai-act/v1": {},
}

type euAIActV1 struct {
	Role               string `json:"role"`
	AnnexIIICategory   string `json:"annex_iii_category,omitempty"`
}

func validateKnownFrameworkPack(id string, payload json.RawMessage) bool {
	if _, known := knownFrameworkPacks[id]; !known {
		return false
	}
	switch id {
	case "eu-ai-act/v1":
		return validateEUAIActV1(payload)
	default:
		return false
	}
}

func validateEUAIActV1(payload json.RawMessage) bool {
	var pack euAIActV1
	if err := json.Unmarshal(payload, &pack); err != nil {
		return false
	}
	switch strings.TrimSpace(pack.Role) {
	case "provider", "deployer", "importer", "distributor", "product_manufacturer", "authorized_representative":
		return true
	default:
		return false
	}
}
