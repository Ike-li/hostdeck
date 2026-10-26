package detail

import (
	"bytes"

	"github.com/Ike-li/hostdeck/internal/plugin"
)

// Value preserves legacy scalar display values and optional localized text.
// Numbers still leave the API as strings, as they did before localization.
type Value struct{ plugin.Text }

func (v *Value) UnmarshalJSON(data []byte) error {
	v.Text = plugin.Text{}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return v.Text.UnmarshalJSON(data)
	}
	var scalar plugin.Scalar
	if err := scalar.UnmarshalJSON(data); err != nil {
		return err
	}
	v.All = string(scalar)
	return nil
}

func (v Value) String() string {
	if v.ByLang == nil {
		return v.All
	}
	if s, ok := v.ByLang["en"]; ok {
		return s
	}
	for _, s := range v.ByLang {
		return s
	}
	return ""
}
