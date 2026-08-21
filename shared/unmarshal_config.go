package shared

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// UnmarshalConfig decodes the host-forwarded plugin config subtree. The host
// sends it as YAML bytes; raw JSON is tolerated too. Decoding routes through
// JSON so struct `json:"..."` tags stay the single naming source.
func UnmarshalConfig(raw []byte, out any) error {
	if len(raw) == 0 {
		return nil
	}
	var node any
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return json.Unmarshal(raw, out)
	}
	asJSON, err := json.Marshal(node)
	if err != nil {
		return err
	}
	return json.Unmarshal(asJSON, out)
}
