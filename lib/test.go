package lib

import (
	"io"
	"os"
	"testing"
)

// CopyFile copies a file from a source to a destination
func CopyFile(t *testing.T, srcPath string, destPath string) {
	t.Helper()
	srcFile, err := os.Open(srcPath)
	if err != nil {
		t.Fatalf("unexpected error opening source file: %v", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("unexpected error creating destination file: %v", err)
	}

	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		t.Fatalf("unexpected error copying source file: %v", err)
	}

	err = destFile.Sync()
	if err != nil {
		t.Fatalf("unexpected error syncing destination file: %v", err)
	}
}
