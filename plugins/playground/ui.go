package main

import (
	"fmt"

	_ "embed"
)

//go:embed ui/index.html
var uiIndexHTML []byte

//go:embed ui/app.js
var uiAppJS []byte

//go:embed ui/styles.css
var uiStylesCSS []byte

func embeddedAsset(name string) ([]byte, error) {
	switch name {
	case "index.html":
		if len(uiIndexHTML) == 0 {
			return nil, fmt.Errorf("asset %q not embedded", name)
		}
		out := make([]byte, len(uiIndexHTML))
		copy(out, uiIndexHTML)
		return out, nil
	case "app.js":
		if len(uiAppJS) == 0 {
			return nil, fmt.Errorf("asset %q not embedded", name)
		}
		out := make([]byte, len(uiAppJS))
		copy(out, uiAppJS)
		return out, nil
	case "styles.css":
		if len(uiStylesCSS) == 0 {
			return nil, fmt.Errorf("asset %q not embedded", name)
		}
		out := make([]byte, len(uiStylesCSS))
		copy(out, uiStylesCSS)
		return out, nil
	default:
		return nil, fmt.Errorf("asset %q not found", name)
	}
}
