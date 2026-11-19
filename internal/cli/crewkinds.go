package cli

// knownKinds are offered in the interactive form.
var knownKinds = []string{"gave_up", "session_blocked", "session_died", "session_failed", "unrouted", "stranded",
	"runtime_down", "runtime_up", "backup_failed", "session_idle", "session_aged", "session_stalled", "session_harness_error", "session_harness_dialog",
	"session_harness_retry", "session_harness_starting", "session_model_mismatch", "session_prompt_gave_up", "session_prompt_lost", "session_prompt_typed", "move_refused", "make_refused", "nudge_held", "note_untold",
	"no_runner", "capped", "workspace_kept", "crew_gave_up", "created", "claimed", "advanced", "closed",
	"held", "waited", "released", "noted", "dep_added", "hook_rejected", "hook_quiet", "folded", "fold_refused"}
