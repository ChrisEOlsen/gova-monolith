package db

import "testing"

func TestOpenTest_WriteVisibleToRead(t *testing.T) {
	d := OpenTest(t, `CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT);`)

	if _, err := d.Write.Exec("INSERT INTO widgets (id, name) VALUES (1, 'a')"); err != nil {
		t.Fatalf("insert via Write: %v", err)
	}

	var name string
	if err := d.Read.QueryRow("SELECT name FROM widgets WHERE id = 1").Scan(&name); err != nil {
		t.Fatalf("select via Read: %v", err)
	}
	if name != "a" {
		t.Errorf("got %q, want %q", name, "a")
	}
}
