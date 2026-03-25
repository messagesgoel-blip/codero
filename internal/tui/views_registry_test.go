package tui

import "testing"

func TestAllViewsMatchesSpecCount(t *testing.T) {
	// Spec §2.1 defines exactly 11 views.
	views := AllViews()
	if len(views) != 11 {
		t.Errorf("AllViews() = %d views, want 11 (spec §2.1)", len(views))
	}
}

func TestViewRegistryNoDuplicates(t *testing.T) {
	seen := make(map[ViewID]bool)
	for _, m := range ViewRegistry() {
		if seen[m.ID] {
			t.Errorf("duplicate ViewID in registry: %s", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestImplementedViewsSubset(t *testing.T) {
	all := make(map[ViewID]bool)
	for _, v := range AllViews() {
		all[v] = true
	}
	for _, v := range ImplementedViews() {
		if !all[v] {
			t.Errorf("ImplementedViews() contains %s which is not in AllViews()", v)
		}
	}
}

func TestViewRegistryDefaultBinds(t *testing.T) {
	expected := map[ViewID]string{
		ViewOverview:   "o",
		ViewSession:    "s",
		ViewGate:       "g",
		ViewQueue:      "q",
		ViewPipeline:   "p",
		ViewEvents:     "e",
		ViewChat:       "c",
		ViewBranch:     "b",
		ViewArchives:   "a",
		ViewCompliance: "r",
		ViewSettings:   "/",
	}
	for _, m := range ViewRegistry() {
		if want, ok := expected[m.ID]; ok && m.DefaultBind != want {
			t.Errorf("ViewRegistry %s default bind = %q, want %q", m.ID, m.DefaultBind, want)
		}
	}
}
