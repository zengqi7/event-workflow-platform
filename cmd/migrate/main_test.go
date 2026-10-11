package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationsWithCompoundTrigger(t *testing.T) {
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("missing migration fixtures", err)
	}
	for _, file := range files {
		statements, err := loadStatements(file)
		if err != nil {
			t.Fatal(file, err)
		}
		for _, sql := range statements {
			if strings.Contains(sql, "DELIMITER") {
				t.Fatal("client directive leaked into SQL")
			}
		}
	}
	statements, err := loadStatements("../../migrations/000004_snapshots.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(statements) != 4 || !strings.Contains(statements[2], "SIGNAL SQLSTATE") || !strings.HasSuffix(statements[2], "END") {
		t.Fatalf("trigger split incorrectly: %#v", statements)
	}
}
