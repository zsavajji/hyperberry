package main

import "testing"

// A sim story, its viewers once each and never a group, and both gone with story.delete.
func TestSimStoryViews(t *testing.T) {
	s, st, _ := linkedSim(t)
	if _, err := s.handleSimStoryView([]byte(`{"n":0}`)); err == nil {
		t.Error("a view with no story of ours: no error")
	}
	res, err := s.handleSimStory([]byte(`{"text":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	id := res.(map[string]string)["id"]
	for _, n := range []string{"0", "0", "2"} {
		if _, err := s.handleSimStoryView([]byte(`{"n":` + n + `}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.handleSimStoryView([]byte(`{"n":1}`)); err == nil {
		t.Error("a group viewing: no error")
	}
	eventually(t, st, 2, "SELECT count(*) FROM story_views WHERE id = ?", id)
	eventually(t, st, 1, "SELECT count(*) FROM story_views WHERE viewer = ? AND name = 'Anna Rossi'", simChats[0].jid)
	if _, err := s.handleSimStoryDelete([]byte(`{"id":"` + id + `"}`)); err != nil {
		t.Fatal(err)
	}
	eventually(t, st, 0, "SELECT (SELECT count(*) FROM stories) + (SELECT count(*) FROM story_views)")
}
