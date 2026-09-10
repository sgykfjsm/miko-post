package cli

// CorrectionPrompt exposes FR-010's prompt selection to the external test
// package.
//
// Its default arm is the reason this seam exists. That arm fires for a rejection
// reason added to internal/post without a prompt here, which is a state the
// external tests cannot construct — Message.Validate returns one of exactly two
// sentinels — so without a seam the arm would be a branch nobody has ever
// executed, in the one function whose job is to make sure a refused message is
// never refused silently.
var CorrectionPrompt = correctionPrompt

// DisplayName exposes the report's name-capitalisation rule.
//
// Reachable through Render only for the two names the sinks actually have, so
// the pass-through arms — an empty name, and the orchestrator's substitute for a
// sink whose Name() misbehaved — need this to be checked at all.
var DisplayName = displayName

// WarningFor exposes FR-076's one-warning selection.
//
// Its close-failure arm has no portable trigger: closing an *os.File succeeds
// unless the filesystem is failing underneath it. Left inline it would be a
// branch nobody has ever run, deciding whether the user is told that their
// diagnostics are broken.
var WarningFor = warningFor
