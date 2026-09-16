package haul

func base() *Haul {
	return &Haul{
		KrakenHaul:    Magic,
		SchemaVersion: SchemaVersion,
		Assignment: Assignment{
			TaskID:     "01JBQ7F3X2K9M4V8N6P0R5T2W1",
			Dive:       1,
			Repo:       "git@git.example.org:brent/yano.git",
			BaseCommit: "4c1d9a7e3f0b2c8d5a6e7f9012345678abcdef01",
			Branch:     "worktree-kraken-01JBQ7F3X2K9M4V8N6P0R5T2W1-1",
		},
		Agent: Agent{Name: "claude-code", Version: "2.1.273"},
		Claims: Claims{
			Outcome: OutcomeCompleted,
			Summary: "added the thing",
			Changes: Changes{Files: []FileChange{{Path: "src/a.go", Change: ChangeModified}}},
			Checks:  []Check{},
			NotDone: []NotDoneItem{},
		},
	}
}

func (h *Haul) with(f func(*Haul)) *Haul { f(h); return h }

// --- the four outcomes, which is what the contract exists for ---------------
