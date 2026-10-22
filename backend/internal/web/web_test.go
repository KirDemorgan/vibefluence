package web

import (
	"io/fs"
	"testing"
)

func TestAssetsIsReadable(t *testing.T) {
	assets := Assets()
	if _, err := fs.ReadDir(assets, "."); err != nil {
		t.Fatalf("embedded assets are not readable: %v", err)
	}
}
