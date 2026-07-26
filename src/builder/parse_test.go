package main

import "testing"

func TestParseFields_Semantic(t *testing.T) {
	got := parseFields([]string{"title:string", "remind_at:datetime", "due:date", "meta:json"})
	if got[1].Type != "string" || got[1].Format != "datetime-local" {
		t.Errorf("remind_at: got type=%q format=%q, want string/datetime-local", got[1].Type, got[1].Format)
	}
	if got[2].Format != "date" {
		t.Errorf("due: got format=%q, want date", got[2].Format)
	}
	if got[3].Format != "json" {
		t.Errorf("meta: got format=%q, want json", got[3].Format)
	}
	if got[0].Format != "" {
		t.Errorf("plain string field must have no format, got %q", got[0].Format)
	}
}

func TestParseFields_Ref(t *testing.T) {
	got := parseFields([]string{"category_id:ref:log_category"})
	if got[0].Type != "int" || got[0].Ref != "log_category" {
		t.Errorf("ref: got type=%q ref=%q, want int/log_category", got[0].Type, got[0].Ref)
	}
}
