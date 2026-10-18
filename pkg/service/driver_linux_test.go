//go:build linux

package service

import (
	"fmt"
	"testing"
)

func TestUnitNotLoaded_IgnoresMissingBinary(t *testing.T) {
	if unitNotLoaded(&InfraError{Msg: "systemctl not found"}) {
		t.Fatal("missing binary was treated as not-loaded")
	}
	if !unitNotLoaded(fmt.Errorf("systemctl: Unit foo.service not found.")) {
		t.Fatal("unit not found was not recognized")
	}
}
