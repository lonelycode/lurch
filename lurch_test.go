package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestWriteToFile(t *testing.T) {
	err := writeToFile(filepath.Join("learn", fmt.Sprintf("conversation-with-%s.txt", "foo")), "bar baz bong")
	if err != nil {
		t.Fatal(err)
	}
}
