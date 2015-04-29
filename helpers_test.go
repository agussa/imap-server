package imap_server

import (
	"testing"
)

func testRange(t *testing.T, rangeStr string, expectedMin string, expectedMax string, expectedErr error) {
	min, max, err := interpretMessageRange(rangeStr)
	if min != expectedMin {
		t.Errorf("Range '%s': min '%s' did not match expected '%s'", rangeStr, min, expectedMin)
	}
	if max != expectedMax {
		t.Errorf("Range '%s': max '%s' did not match expected '%s'", rangeStr, max, expectedMax)
	}
	if err != expectedErr {
		t.Errorf("Message range %s\n"+
			"\tExpected error: %s\n"+
			"\tActual error: %s", rangeStr, expectedErr.Error(), err.Error())
	}
}

func TestFindMessageRange(t *testing.T) {
	testRange(t, "15:95", "15", "95", nil)
	testRange(t, "53:*", "53", "*", nil)
	testRange(t, "35", "35", "", nil)
	testRange(t, "5*", "", "", errInvalidRangeString)
	testRange(t, "*5*", "", "", errInvalidRangeString)
	testRange(t, "hello", "", "", errInvalidRangeString)
}
