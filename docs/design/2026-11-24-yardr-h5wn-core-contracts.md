# The contracts of yardr's core: what the core is, what crosses its edge, and where the rules are declared

Design yardr-h5wn, proposed 2026-11-24. A proposal as it was decided on, not a description of what the code does today; the decision is on the bead.

This text was redrafted on 25 Nov 2026 on mckean's decisions of 24 Nov (asks 30918, 30920, 30922, 30923 and 30925 on the bead, each answered with its first option) and his mail 246738, and again on 25 Nov on his answer to ask 31498, which decided the harness-file question. Section 5 lists those decisions, and the sections they change say them in place.

A **bead** is one durable piece of work. A **flow** is the stages a bead passes through, and a **stage** is one step of it. A **session** is one agent (or script, or person) working one bead in one stage, and the **runner** is how a session starts: `herdr` (an agent program in a terminal pane that the herdr multiplexer runs), `exec` (a command), `manual` (people) or `peer` (another yard). A **harness** is the agent program a herdr session runs (Claude Code, Codex, Kimi); its **kind** is the name herdr starts it by. A **source** is an outside service the yard calls or hears from, declared in a file. A **pack** is a way of working as one directory, imported into a depot or into the yard. An **ask** is a question stored as data on a bead. A **run ask** proposes one yardr command, and its **card** is what the person approving it reads: the command whole and the yard's sentences for what it would change. A **peer** is another yard this one is linked with. **footplate** is the desktop app, kept in its own repository, that reads a yard over its socket API. The **gate** is the depot's test command, `sh .yardr/test`. See `docs/glossary.md` for all of yardr's words.

In this document the **core** is the part of yardr that the contracts are written around, and the **how-and-what** is everything that is not core. A **contract** is a form that crosses between the two: data, a file format, a command, an RPC call or a Go interface. A contract has an **owner**, a **version** and a **change rule**, which says what a change to it must do. A **norm page** is a page in the repository that states a contract's rules, and a **check** is a test the gate runs that fails when the code breaks one of them.

Every size below was measured at 635f47b7b (main, 24 Nov 2026) unless it names another commit, and each one names the command that produced it. Commands that read a tree were run over `git archive 635f47b7b internal harnesses packs | tar -x` in a scratch directory, from its root. Where this document says "the measure" without a command, it means this one:

```sh
# lines of non-test and test Go per package directory
for d in $(find internal -type d); do
  p=$(find $d -maxdepth 1 -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l)
  t=$(find $d -maxdepth 1 -name '*_test.go' -exec cat {} + | wc -l)
  echo "$d $p $t"
done
```

## 1. The question

mckean, 24 Nov 2026 (mail 246477): "I think we need to create interfaces, contracts, apis. rules that yardr shares, the core should be slim, event broking, handling messages, delivering updates, managing, running beads. the how and what should be external." He floated a new project built with what has been learned, since "the agents cannot be drastic enough". The order agreed in mail 246501 ("let's start with 1") has three steps. Step 1, this design, writes the contracts. Step 2 sorts the code against them and measures how much is core. Step 3 uses that number to choose between a new core beside the old one and cutting in place. This document is step 1 only. It moves no file and does not make the step 3 choice.

## 2. What exists today

### 2.1. Sizes

The measure at 635f47b7b gives the bead's own figures again. The whole module is 262,677 lines of product Go and 434,982 of tests (`git ls-files '*.go' | grep -v _test.go | xargs cat | wc -l`, and the same over `'*_test.go'`). By package, product / test lines and product files:

| package | product | test | files |
|---|---:|---:|---:|
| internal/store | 101,490 | 119,759 | 260 |
| internal/cli | 54,370 | 83,428 | 135 |
| internal/dispatch | 18,044 | 40,057 | 55 |
| internal/pack | 11,169 | 15,422 | 27 |
| internal/web | 10,589 | 23,737 | 35 |
| internal/runner | 8,051 | 17,008 | 25 |
| internal/source | 7,869 | 7,639 | 24 |
| internal/rpc (with rpc/clientgen and rpc/gen) | 7,260 | 18,305 | 21 |
| internal/server | 5,456 | 11,365 | 20 |
| internal/hooks | 3,750 | 13,185 | 17 |
| everything else (25 other internal packages, main, contrib, packs, docs) | 34,629 | 85,077 | — |

`store.API`, the interface every process uses to reach the store (`internal/store/api.go:11`), has 328 methods (`grep -cE '^\t[A-Z][A-Za-z0-9]*\(' internal/store/api.go`). `docs/architecture.md` still says 296 in its split section and 310 in its layers section. Both numbers are stale. The layers figure is not under `TestLayersMatchCode`, which checks package names and the import count, not the method count.

### 2.2. What internal/store holds now, and why the import rules did not stop it

Design yardr-p326 (`docs/design/2026-10-14-yardr-p326-codebase-layout.md`) counted internal/store at 35,108 non-test lines in 59 files at 2469fd9 and decided to keep it one package. Re-measured at the last main commit of each date (`git rev-list -1 --before="<date> 23:59" main`, then `git show <c>:<f> | wc -l` over the non-test `.go` files that `git ls-tree --name-only <c> internal/store/` lists, and over those named `runask*.go`):

| date | commit | internal/store | of which runask*.go |
|---|---|---:|---:|
| 14 Oct | bc9ff07fb | 39,108 | 1,401 |
| 29 Oct | 80668167e | 70,024 | 10,910 |
| 31 Oct | 3eae996b2 | 75,874 | 12,671 |
| 7 Nov | 768dda852 | 77,644 | 13,379 |
| 14 Nov | 10d47ed44 | 90,002 | 18,888 |
| 24 Nov | 635f47b7b | 101,490 | 23,038 |
| 25 Nov | fdde0e705 | 101,073 | 21,238 |

The last row is after yardr-mzy7 landed (7b62f0784, 24 Nov), which deleted 13 run-card types. Every other figure in this document is at 635f47b7b.

632 of the 1,911 commits on main since 14 Oct touched internal/store (`git log --since=2026-10-14 --format=%h main [-- internal/store] | wc -l`, at 635f47b7b). Grouping the net growth by file family:

```sh
git diff --numstat bc9ff07fb 635f47b7b -- internal/store | grep -v _test.go |
  awk '{f=$3; sub(".*/","",f); fam="other"
    if (f ~ /^runask/) fam="runask"; else if (f ~ /^peer/) fam="peer"
    else if (f ~ /^(pair|device)/) fam="pairing"; else if (f ~ /^delegation/) fam="delegation"
    else if (f ~ /^(harness|promptlook|loginsteps|providerterms)/) fam="harness"
    n[fam]+=$1-$2; c[fam]++} END {for (k in n) print k, n[k], c[k]}'
```

- **run-ask cards, +21,637 lines in 59 files.** One file per yardr command a card can propose. At 635f47b7b, `runList` holds 32 rows (`internal/store/runask.go:237-586`, `grep -cE '^\s+words: '`), and 18 files add 19 more through `init()` (`git grep 'runList = append(runList'`; `runask_route.go` adds two), so there are 51 rows. At fdde0e705 there are 40: 25 in the literal and 15 appended, by the same two commands. Each row is a `runCommand` (`runask.go:148-230`): the command's words, a parser, a check, a card that writes the yard's sentences, a weight, a staleness test and the run itself. The cards for setting up a pack (`runask_setup*.go`) and the pack-card files (`runask_pack*.go`, `delegation_pack*.go`, beside `packs.go` and `packmappings.go`) are part of this family or sit beside it.
- **peers and pairing, +4,375 in 29 files** (the `peer` family, +1,922 in 22 files, and the `pairing` family, +2,453 in 7). Peer files are now 23 files and 11,387 lines (`ls internal/store/peer*.go | grep -v _test`, and `cat` of them through `wc -l`). Pairing and device files are 3,601 lines by the same command over `pair*.go` and `device*.go`.
- **the yardmaster's delegated runs, +2,918 in 6 files** (`delegation*.go`).
- **harness knowledge, +2,448 in 11 files** (`harness*.go`, `promptlook.go`, `loginsteps.go`, `providerterms.go`).
- **everything else, +31,004 in 143 files.** This includes `policy.go` (1,164, the permission gates), `claim.go`, `weight.go`, `fold.go`, `footplate.go` (726, the push queue for footplate's notifications), `line.go` (454, "one depot's line as footplate's Line window draws it", `line.go:1`), `flowsboard.go`, `flowmap.go` and `decisions.go`, and the source records with the hosted Google setup: 13 non-test store files name Google (`grep -l Google internal/store/*.go | grep -v _test`), among them `googlecred.go`, which writes a Google-specific accounts file, and `sourcecalls.go:76`, which has `CallHostedGoogle`.

The import rules did not stop this growth because they say only what the store may import. They say nothing about what it may contain. Rule (a) of `docs/architecture.md` ("`internal/store` imports no `internal` package", with exceptions for `peer` and `source`) was put under a test on 29 Oct (yardr-x0gg, 72abae04c). Each import the store already had was then removed by moving the imported code into the store:

- `internal/shared` and `internal/udiff` were folded in (yardr-g1sf, 6441473be, +371 lines net).
- The harness types moved from `internal/agents` (yardr-8nca, 0f542995e, +1,359). `internal/agents` now imports the store. The store checks kinds, presets and labels against tables it copies from the harness files: `HarnessLabels`, `HarnessLoginEnv`, `HarnessPresets`, `HarnessOffers` and `HerdrKinds` in `harness_kinds.go`, `HarnessEfforts` in `harness_effort.go:136`, and `HarnessModelEnvs` and `HarnessNoModelFlag` in `harness_model.go`. `TestStoreTablesMatchCatalogue` (`internal/agents/catalogue_test.go:15`) holds all of them to the files except `HerdrKinds`, and `internal/source/limits.go:13` keeps a second copy of `HerdrKinds`.
- The key and pairing types moved in (yardr-rxk9, e4bc912f5, +479).
- The config reads moved in (yardr-t8m6, 1fff77238, +379).

The line counts are each commit's own (`git show --numstat <commit> -- internal/store`, added minus removed over non-test files; lines added alone were 432, 1,412, 564 and 489). Each move kept the rule and grew the store. The one move the other way, yardr-sfz1 (b71be933c, 29 Oct, which took the store's eta code out: 1,011 lines removed and 54 added, −957 net by the same command), shows that the rule can push code outward too. Nothing in the rule prefers outward over inward. The store grew by 62,382 lines net between the 14 Oct and 24 Nov rows of the table (101,490 − 39,108). The four moves inward account for +2,588 of that (371 + 1,359 + 479 + 379) and the move outward for −957, so the moves together are +1,631, and the remaining net growth is 60,751 (62,382 − 1,631). That remainder is net of everything else the 632 commits did, new features and deletions alike (yardr-mzy7's deletion of card types, for one, lies after the 24 Nov row); this document does not split it further, and the file families above show where it sits. Most of it was written into the store because the store is where a record and its rules meet in one transaction. That is the reason p326 gave for keeping the store whole (p326, sections 2.2 and 4). A run ask is the clearest case. Approving it re-draws the card, compares the fingerprints and runs the command in one write (`approveAsk`, `runask.go:809-960`), so each new command's card and run were written beside that write.

### 2.3. What is already outside, and in what form

- **Packs.** These are the directories under `packs/` that `packs/packs.go:38` embeds, plus the yardr-packs repository, which is the only allowed origin for a delegated pack apply (`pack.Repository`, `internal/pack/repository.go:17`). `pack.yaml` carries `api:`. The current number is `const API = 8` (`internal/pack/pack.go:39`). A pack that asks for a higher number is refused before any other key is read (`pack.go:560-574`). A pack's `kind:` is one of `pack.Kinds`, the depot kinds `git` and `dir` (`pack.go:284`), or `pack.KindSource` (`source`, `pack.go:289`) for a pack imported into the yard itself, or none. `docs/pack-api.md` marks each part stable or internal, and `TestPackAPIDoc` (`internal/cli/packapi_test.go`) writes its generated sections.
- **Sources.** A declaration file has 20 known root keys and refuses any other at every level (`internal/source/declaration_parse.go:62-90,149`). It has no version field. Provider logic lives in commands the declaration names: a call that names `run` executes an argv with JSON on stdin and one JSON object on stdout (`internal/source/command.go:25-38`), and pack scripts such as `packs/linear/linear/normalize` and `packs/github/github/thread` do that work. The hooks package compiles only two adapters, `routine` and `token` (`internal/hooks/hooks.go:153-156`). Every other provider is a declared source. Provider names still appear in Go code: the 13 store files that name Google (2.2), and many cli files (help text and examples).
- **Flows and stages.** These are files: `.yardr/flows/<name>.flow`, `.yardr/flows/type/<type>.flow` and stage files `.yardr/flows/<name>/<stage>.md` (`internal/store/repoflows.go:22-57`), read by `store.ParseFlow` (`flows.go:340`). There is no version field. The outcome vocabulary is open, constrained only by a name pattern (`validOutcome`, `flows.go:71`), and only `done` and `question` are constants in the code.
- **Role files and sections.** `roles/<name>.md` is looked up in the depot's `.yardr/roles/` and then in the yard's home (`internal/runner/runner.go:468-476`). A pack's `section:` file reaches the yardmaster at prime time (`internal/cli/prime_sections.go:53`).
- **Harness files.** There are nine, `harnesses/<kind>.yaml` (agy, claude, codex, grok, kimi, kiro, muse, opencode, pi), embedded by `harnesses/harnesses.go:15-17` and read from nowhere else (`agents.Load(harnesses.FS)`, `internal/agents/harness.go:43`), at `version: 1` (`store.FileVersion`, `internal/store/harness.go:17`, checked in `Harness.Check` at 752). Their content is outside the Go code. What they mean is not:
  - nine non-test sites branch on a kind literal (`runner/trust.go:178,228`, `runner/herdr_brief.go:27`, `runner/herdr.go:864`, `agents/harness.go:219,221,237`, `cli/userlogin.go:556`, `cli/doctor.go:516`), found with `K='agy|claude|codex|grok|kimi|kiro|muse|opencode|pi'; grep -rnE "(case|==|!=) *\"($K)\"" --include='*.go' internal | grep -v _test.go`;
  - 28 functions carry a kind in their name: 15 model listers and parsers in `agents/models.go`, three in each of `runner/trust.go`, `cli/codextrust.go` and `cli/grouplogin.go` (the `claudeAccount*` functions), two in `cli/kimihooks.go`, and one each in `runner/herdr.go` and `cli/doctor.go`, found with `grep -rnE '^func (\([^)]*\) )?[A-Za-z]*(Agy|Claude|Codex|Grok|Kimi|Kiro|Muse|Opencode|OpenCode|claude|codex|kimi|grok|kiro|muse|agy|opencode)[A-Za-z]*\(' --include='*.go' internal | grep -v _test.go`;
  - two test files are about one harness each (`cli/codextrust_test.go`, `cli/kimihooks_test.go`), and 26 test functions carry a kind in their name (`grep -rhE '^func Test[A-Za-z0-9_]*(Agy|Claude|Codex|Grok|Kimi|Kiro|Muse|Opencode|OpenCode)' --include='*_test.go' internal | wc -l`).
- **BDP conformance.** `conformance/bdp/` pins the provider kit at one commit and runs it against `yardr bdp serve` (`probe-core.sh`). CI runs the same pin (`.github/workflows/release.yml:95-109`). This is the one contract with an outside test suite.

The harness design yardr-tpb2 (`docs/design/2026-10-17-yardr-tpb2-harness-packs.md`) proposed a pack kind `harness` with build-pinned fields. It was decided on 17 Oct and never built: `harnesses/` is still embedded, and no pack kind `harness` exists (`pack.Kinds` and `pack.KindSource` above). mckean now doubts that a pack is the right form (mail 246467). He wants a harness API in the core, per-harness configuration and matchers outside it, and no per-harness tests in Go.

### 2.4. Where rules are declared today, and which have a check

- `docs/architecture.md` is paired with `arch_test.go`: `TestImportRules` holds rules (a) to (c), and `TestLayersMatchCode` holds the layer lists.
- `docs/api.md` is paired with `TestAPIDoc` and `apiCalls` (`internal/rpc/api_test.go`). `rpc.Calls` (41 names, `internal/rpc/api.go:46`) and `rpc.OperatorCalls` (14, `api.go:231`) are the 55 calls a program outside yardr may rely on, and "A name here is on the wire for good" (`api.go:35-37`). A program says what it is in the `X-Yardr-Client` header (`rpc.ClientHeader`, `api.go:14`), and "Nothing is decided by it".
- `docs/pack-api.md` is paired with `TestPackAPIDoc`.
- `docs/groups.md`'s harness tables are written from the harness files (`internal/agents/doc_test.go`).
- The per-file line ceiling is the `filesizes` table in `.yardr/test` (line 2161): a file over 1,000 lines may not grow. It caps files, not packages, so it pushes a growing package to make more files. internal/store went from 59 files to 260.
- `docs/peers.md`, "Two yards on different builds" (line 1181), states the peer rule: each yard says with every signed request which kinds its build knows, an unknown kind is refused as `kind_unknown` and "no `peer allow` mends it, only an upgrade", and a body whose shape changes carries a `v`. No single test is named as its check.
- AGENTS.md, `.yardr/depot.md` and the briefs' role sections (the "Builder" and "Working in yardr" text a session is given) repeat rules in prose. The cli rule that p326 decided ("A command parses flags, asks the operator, and prints") lives only in the builder brief and has no check.

## 3. The proposal

### 3.1. The core, by a rule that sorts

Two questions put any file on one side. They are written for Go, where the sorting is measured (step 2 counts Go only), and the end of this section says how they place every other file.

**Q1. Does it keep, deliver or move a record of the yard?** A record is a bead, note, ask, answer, event, mail, session, flow, route, group, depot, source record, peer record, user or setting. Keeping means storing it, migrating it and deciding who may change it. Delivering means a subscriber, an inbox or a push learning of it. Moving means the flow engine taking a bead from stage to stage, or the dispatch pass starting, watching and ending a session through the runner interface.

**Q2. Does it know something that changes when something outside yardr's own model changes?** That means a harness program and its versions, herdr's commands, a service's API, the words, flags or sentences of one yardr command, or the layout of one page, window or terminal view.

A file is **core** when Q1 is yes and Q2 is no. A file is **how-and-what** when Q2 is yes. A file where both are no is a library (config, version, otel, yardkey, the rpc transport). It sits with whoever imports it, and with the core when the core imports it.

By this rule, the following are core: the store's transaction, bucket and event machinery, beads and their gates, notes, asks and answers as data (the `Ask` record, approve, put aside, decline, take back), the carry record that an approved command waits in (`internal/store/carry.go`), mail and notifications, sessions and claims, flows and their moves, routes and groups as records, permission gates (`policy.go`), migrations, the dispatch pass, the runner interface, peering as delivery (3.2.8), and the store API with its socket.

The following are how-and-what: harness knowledge in any package, the herdr runner's pane reading, every run-ask row's parser, card sentences and run, pack content and the pack planner, source declarations' provider logic, the web pages, chat, mcp's tool wording, doctor, onboarding (`init`, `setup`, `guide`), upgrade and release, and the daemon's housekeeping passes that know about the machine (`gocache.go`, `diskkeep`, `telemetry.go`).

**Where the rule cannot decide.** These cases are named so that step 2 counts them as mixed rather than forcing a side. Each is mixed inside one file, and the sorting measures it by function:

1. **The run-ask machinery.** Proposing, approving, re-drawing and staleness are core: an approval moves state. The rows are how-and-what: each knows one command's words. They are written into the same functions, and `approveAsk` calls a row's `card` and `run` inside the core's write (`runask.go:902-933` at 635f47b7b, `833-864` at fdde0e705).
2. **Routing by quota.** `Route.Choose` is core. The quotas it is judged by come from a harness subscription's limits (`internal/limits`, `dispatch/quota.go`, `harness_limits.go`), which is how-and-what. `Route.Quotaed` (`routing.go:249-253`) reports whether a route names a further group or carries a condition, "what only groups that start agents may have", and `Route.Gated` (`routing.go:264`) asks whether choosing reads windows because a group starts agents: both lean on a fact about the runner.
3. **Reads shaped for one view.** `line.go`, `flowsboard.go`, `flowmap.go` and `decisions.go` assemble records (core) in the shape of one footplate window or web page (Q2). They live in the store because the read must be consistent in one transaction.
4. **The prompt a session is given.** `promptlook.go` says, in its own first lines, that it lives in the store only because the runner imports the store. Choosing the stage file is core. Reading a brief for one harness is how-and-what.
5. **Peers.** Peer records, the outbox and settlement are core: delivery to another yard. The wire format, signing and sealing are a contract (3.2.8). Peering stays in the core (decision 4 in section 5).
6. **The dispatch harness passes** (`dispatch/harness*.go`, 3,224 non-test lines at 635f47b7b and at fdde0e705, by `ls internal/dispatch/harness*.go | grep -v _test.go | xargs cat | wc -l`). They ride the core's pass and record on the core's session handle, but what they match is harness output.
7. **Housekeeping.** Backups and pruning of the store are core (Maintenance). GOCACHE rotation and the disk floor are not, although `dispatch` runs them.

**Files that are not product Go.** The same two questions place them, with one rule for each kind of file:

- **A test file** (`*_test.go`) takes the side of the code it tests. A test of a core function is core, and a test of one harness, one command's card or one page is how-and-what. A test file that tests both sides is mixed, like the file it tests. The fixtures a test reads (`testdata/`) go with the test.
- **A file the core reads as data** is how-and-what when it holds knowledge of something outside (Q2): the harness files (`harnesses/*.yaml`), the packs under `packs/` with their flows, declarations and scripts, the yard's own `.yardr/flows/` and `.yardr/roles/`. The format such a file is written in is the core's, and its norm page is the core's (3.3), but the file itself is not.
- **A page** is on the side of what it describes: `docs/contracts/` and the store's migrations are core, a user page about one command, pack or service is how-and-what. A design document is neither; it is a record of a decision.
- **Scripts and tools** outside the binary (`contrib/`, `install.sh`, `.yardr/test`, the release workflow) are how-and-what, except that the gate's checks of the core's rules (3.3, 3.4) carry the core's norm and sit with it.

Step 2 measures Go only, product and test, because that is where the core's size is and where a ceiling can count. The rule for other files is written down so that the norm pages and a later table, if step 3 builds one, place them the same way.

### 3.2. The contracts at the core's edge

One section each: the form that crosses, its owner, its version today and proposed, its change rule, and the stored data it touches (section 3.5 explains why that last part matters). "Owner" means who may change the contract's text. Contracts that are internal (Go interfaces inside this module) have the core as owner and need no wire version: a change updates every implementation in the same commit, as mem-y81p allows.

One rule holds for every contract with a version, by mckean's mail 246738: "outside dependencies like packs are required to look at the changes of the yard. yardr core dictates how things work. we are early we can move fast and break things." The core changes a contract whole, in one release. Whatever sits at the other end (a pack, a declaration, a depot's flow, footplate, a linked yard on another build) follows, and until it does, the core refuses it with a plain reason that names the version it needs. No old form is kept readable beside the new one, and no contract has a grace period.

#### 3.2.1. Harness: what the core asks of an agent program

- **Today.** The harness file (`harnesses/<kind>.yaml`, `version: 1`, 22 root keys, unknown keys refused by `KnownFields(true)` in `agents.Load`, `internal/agents/harness.go:101`), plus the Go that interprets it in store, agents, runner, dispatch and cli, plus the per-kind Go listed in 2.3.
- **Form.** Two parts. (1) A **harness interface** in the core: the few questions the core asks of a session's program, as data the core reads, never as harness names. Is it at its prompt? Did it stop with an error the yard knows, and which? Which dialog is on screen, and may the yard answer it? Which model and how much context is it using? What arguments start it unattended? (2) A **harness file format**, owned by the core, that answers those questions per kind by matchers (screen marks, patterns, answers, argument lists) and declares the capabilities a kind has. A kind's behaviour that is code today (the model listers, the `seedClaude*` home edits, `codextrust.go`, `kimihooks.go`) becomes either a matcher in the file or a command the file names, run as a source command is run (argv, JSON out). It is tested by fixture files beside the harness file (a recorded pane and the answer expected), not by Go per kind. tpb2's pack form is dropped (decision 4).
- **Reviewed bytes.** The fields of a harness file that become arguments of the program, answer a dialog on its screen, or name a command the yard runs come only from reviewed bytes: today, the files built into the binary. This is tpb2's security rule, carried over, and the harness norm page states it with its check.
- **A yard's own harness file** (decision 6, ask 31498): "A yard may carry its own harness file for matchers and models, never for what starts a program or answers its dialogs." A file in the yard's home beside the built-in ones may hold matchers (screen marks, prompt and error patterns) and models, for a kind the build carries or a new one. A key that becomes an argument of the program, answers a dialog on its screen or names a command the yard runs is refused in it, with the key's name and the reason, and is taken only from the build's file of that kind. So a kind yardr has never carried can be watched and read at once, and is not started by the yard until its starting arguments are in a release. The norm page lists which keys are guarded, and its check is a test that loads a yard file carrying each guarded key and expects the refusal. A yard file is read at the format's one version, as the built-in ones are, and is refused at another.
- **Owner.** The core owns the interface and the file format. Each harness file has its own owner: the yardr repository today, and possibly a separate one later, which is a step 3 question.
- **Version.** The file's `version` (`FileVersion`) stays the format's version. A change that adds an optional key keeps the number. A change that removes or re-means a key raises it, every file the build carries moves with it, and the core reads exactly one number: a file at another number is refused with the number it needs (mem-y81p: no shim).
- **Change rule.** A new question in the interface lands with its key in the format, its answer in every file the repository carries, and a fixture per kind. A new kind is a file plus fixtures, with no Go.
- **Stored data.** Group config (`args`, `model`, kind), `Session.Kind` and `Session.Model`, and the handle keys dispatch writes (`harness_error`, `harness_dialog`, `harness_fallback_model` and the others listed at `dispatch/harness.go:76-102`). No harness file is stored: a yard's own file is a file in its home, read at start, not a record.

#### 3.2.2. Source: calls and hooks

- **Today.** The declaration format (2.3), the command protocol (`source.Run`: argv, JSON on stdin, one JSON object of at most 64 KB on stdout), and the hook envelope (`hooks.Envelope`, `hooks.go:35`). A declared hook's `normalize` command receives `{"headers","body"}` and returns an envelope (`internal/hooks/declared.go:606-631`). The store records it as a `hook` event (`internal/store/hooks.go:1020`).
- **Form.** A file format (the declaration), a process protocol (the command's stdin and stdout) and a data shape (the envelope and the event it becomes). This is already the right shape: the provider logic is outside, in commands.
- **Owner.** The core owns the format and protocol. Pack authors own declarations.
- **Version.** None today. Proposed: a declaration carried inside a pack is versioned by the pack's `api:` number, and the norm page says so. A standalone declaration (`yardr source add <file>`) is read at the running build's format. Either way, a declaration the build cannot read is refused with its reason (the key or line it cannot read, and the pack API the build reads), and its author follows the core.
- **Change rule.** As for the pack API (3.2.5): the change is whole, and every declaration the repository carries moves with it. The provider-specific Go that remains (`HostedGoogleMint`, `googlecred.go`, `CallHostedGoogle`) is how-and-what inside the core, and step 2 counts it.
- **Stored data.** Source records with their stored declarations, grants, keys and credential files, the call log, hook deliveries, links and threads. Of all the contracts, this one touches the most stored data, so a format change must come with a migration of the stored declarations.

#### 3.2.3. Runner: starting and reading a session

- **Today.** `dispatch.Runner` (`internal/dispatch/dispatch.go:94`): `Start`, `Alive` and `Stop`. Beside it, dispatch's non-test files declare 27 more interface types (28 in all, by `grep -hE '^type [A-Z][A-Za-z]* interface' internal/dispatch/*.go` over the non-test files), most of them optional interfaces a runner may implement, one question each: `Statuser`, `Waiter`, `Finisher`, `Waker`, `Tailer`, `Versioner`, `TrustAcceptor`, `DialogReader`, `CrewRunner` and others. The implementations are registered in `runner.All` (`internal/runner/runner.go:41-47`): manual, exec, herdr and `store.PeerRunner`. Herdr is reached by its CLI and socket (`herdr_api.go`, `herdr_events.go`) and assumed at 0.9, with the only version check in `cli/needs.go:489`, not in the runner. The `*Herdr` runner has 35 exported methods.
- **Form.** A Go interface (internal), plus the session handle, a `map[string]any` stored on the session.
- **Owner.** The core.
- **Version.** None, being internal. Proposed: the norm page lists the interface and each optional interface with the one question it answers. A new optional interface needs a line there, so the list does not grow unseen. The herdr runner names its herdr version in one place and checks it at start, since herdr is an outside program, and refuses a herdr it does not read with the version it needs.
- **Change rule.** Change every implementation in the same commit. A new handle key is listed on the page with the runner that writes it.
- **Stored data.** Sessions and their handles. Live sessions survive a server restart and an upgrade, so a handle key written by the old build must still be readable by the new one, or the upgrade must end the sessions it cannot read.

#### 3.2.4. Flow and stage

- **Today.** The flow DSL or JSON (`ParseFlow`), the repository layout (`repoflows.go`), stage files as Markdown, pack `flows:` YAML (`pack.go:141-161`), and an open outcome vocabulary.
- **Form.** File formats, read by the core's flow engine.
- **Owner.** The core owns the formats and the engine. Depots and packs own their flows.
- **Version.** None. Proposed: a flow that arrives in a pack is versioned by the pack API. A flow in a depot's `.yardr/flows/` is read at the running build's format, which the norm page names. A flow the build cannot read is refused with the line and its reason, as `ParseFlow` refuses one today, and the depot follows the core.
- **Change rule.** A change to the DSL updates every flow the repository carries and migrates the stored flows. A new outcome needs nothing, since the vocabulary is open. A new constant outcome (as `question` is) is a core change and goes on the page.
- **Stored data.** Flows, flow pointers, each bead's stage and its history in events. A stage renamed in a flow strands beads at that stage. `Stranded` (Flows, `docs/architecture.md`) already reads that case.

#### 3.2.5. Pack

- **Today.** `pack.yaml` at `api: 8`, the kinds of 2.3, `docs/pack-api.md` with stable and internal parts and a per-number list of additions, the import record (`store.PackImport`), and the pack planner (`internal/pack`, 11,169 lines).
- **Form.** A file format and directory layout, plus the commands and scripts a pack may call (pack-api.md, "What is stable").
- **Owner.** The core owns the API. Pack authors own the content.
- **Version.** `api:` as it is. This is the model the other contracts copy.
- **Change rule.** A rise of the number for each addition to the stable part, as pack-api.md says today, and for each removal or change of meaning. The core reads the numbers it reads and no others: an outside pack (from yardr-packs or a URL) at a number the build no longer reads is refused before it is planned, with its reason and the number the build needs. The pack's author follows the core. No old form is kept for it, and there is no date by which an old form goes, because it goes in the release that changes it (mail 246738).
- **Stored data.** Import records with each object's sha256, pack mappings, and the records a pack becomes (flows, groups, routes, sources, crew). The records stay when a pack's number is refused: a refused pack is one that cannot be applied again until it follows, not one whose earlier records are undone.

#### 3.2.6. Card and ask

- **Today.** `Ask` (`internal/store/asks.go:64-156`) and `Answer` (`asks.go:164-189`) are stored as notes. There is no Card type: a run card is `AskRun` (`runask.go:69-143`), whose `Does []string` holds the yard's sentences and `Seen` holds the fingerprints approval compares. Clients receive `RunAsked` (`runask_proposed.go:32-52`). The rows of `runList` (51 at 635f47b7b, 40 at fdde0e705, 2.2) each carry their own parse, card and run. Approved work that runs after the write waits in the `carry` bucket and is carried out by the server (`internal/server/carry*.go`, 2,807 non-test lines at 635f47b7b and 2,760 at fdde0e705, by `ls internal/server/carry*.go | grep -v _test.go | xargs cat | wc -l`).
- **Form.** Two contracts. (1) **The ask**, which is core data: question, options, recommendation, answer, the moves it may make, and the card as data (`Does`, `Seen`, `Plan`), which every view reads and renders without knowing the command. (2) **A card row interface**: what one command must supply to be proposable. That is its words, a parse into stored arguments, a check, the sentences for the card, a fingerprint of what it reads, and a run. The core keeps the ask, approval, staleness and carry machinery and defines the row interface (decision 3). The rows are how-and-what and register against it. They live beside the command they card (the cli or the package the command calls), not in the store. This is what `runCommand` (`runask.go:148`) already is, written down and moved to the edge, with one change its move forces, below.
- **The write a row runs in.** Today a row's functions fall in three kinds (`runCommand`, `runask.go:148-230` at fdde0e705). Those that read only the typed words take no store: `parse(rest)`, and `whose`, `sole` and `link`, which read the parsed argv. Two read before the write: `ready` and `fetch` take the `*Store` (`runask.go:219,224`), and call its reads (`s.RepoFlows`, `s.Depots`, the private `s.repoFor`, and `s.packFetcher`, the field the server sets after it opens the store), because git and the packs repository must not be read inside a write. `ApproveAsk` runs them first (`approvalRepos`, `runask.go:721` at fdde0e705) and hands what they read into the write as `w.repos` and `w.urlPack` (`runask.go:727`). The rest are bound to the write: `check`, `card`, `weigh`, `stale` and `plan` take the store's private write, `*wtx` (`store.go:432`), `run` takes it and the private `*approvedOn` (`runask_proposed.go:104`), and so do the forms that take the ask whole (`checkRun`, `cardRun`, `runRun`). `approveAsk` (`runask.go:740`, fdde0e705) draws the card again, compares it with the stored one, weighs it, and runs the row, all inside the one write `ApproveAsk` opened (`runask.go:693-728`): the yard cannot change between the check and the run, and an error anywhere rolls back, so nothing ran. A package outside the store cannot name `*wtx`, so the interface needs a write it can be handed. The contract keeps the call path and changes only the type that crosses it:
  1. The store exports a write handle, `store.Tx`, an interface. A row receives it as an argument and never makes one: it has no method that opens, commits or keeps a write, and the store hands it out only inside its own write. Its methods are the in-write forms of the store functions that rows call today (`w.renamePeer`, `w.setSource` and the others), each the same function its `store.API` method runs inside a write of its own, and the reads a card makes of records (today a row reads buckets directly, as `runPeerRename` reads `bCrew` through `each`, `runask_more.go:306-330`). `approvedOn` becomes the exported `ApprovedOn`.
  2. The row is a Go interface in the same three kinds. Without a store: `Words() []string`, `Use() string`, `Description() string`, `Parse(rest []string) ([]string, error)`, and the optional `Whose(argv) string`, `Sole(argv) string`, `Link(argv) (peers []string, rule string)` and `Commas() bool`. Before the write, optional, on a narrow read handle `store.Before` and never on `Tx`: `Ready(ctx, b store.Before, run AskRun, argv []string) (map[string]*RepoFlows, error)` and `Fetch(ctx, b store.Before, argv []string) (PackReader, error)`. `Before` holds only the reads these call today (`RepoFlows`, `Depots`, the repository a peer bead's flow is read from, and the pin-checked pack reader), and it opens no write. What they return the store hands into the write as it does now, and the row reads it there through `Tx` (`tx.Repos(depot)`, `tx.URLPack()`), never through a second read. Inside the write, on `Tx`: `Check(ctx, tx, argv, actor) error`, `Card(tx, argv, asking) (AskRun, error)`, `Weigh(tx, card) ([]AskWeight, error)`, `Stale(tx, seq, was, now AskRun) error`, `Run(ctx, tx, argv, actor, by *ApprovedOn) (string, error)`, and the optional `Plan(tx, run)` and the forms that take the ask whole (`CheckRun`, `CardRun`, `RunRun`, with `run AskRun` in place of `argv`).
  3. `ApproveAsk` is unchanged in order and in what it holds: it opens the write, finds the row by the stored argv's words, calls `Card` on the handle, makes the comparison of `Does`, `Seen`, `Back` and `Plan` itself (the core's, not the row's), calls `Stale` where they differ, weighs and gates, calls `Run` on the same handle, records the carry and the answer, and commits. Asking (`card` with `asking` true, in the write that stores the ask) keeps its path the same way.

  What it costs: `store.Tx` is a new exported surface, as wide as what the rows call. The norm page lists its methods, and a new one is a line there. Step 2 counts its width at fdde0e705 (the distinct store functions and buckets the 40 rows' functions reach). A row that calls `store.API` from inside `Run` would open a second write inside the first, which bbolt's one writer does not allow, so the interface gives a row no `API` and the page says why. The alternative of running rows through `store.API` after the approving write is left out: the yard could change between the check and the run, and that recheck is what makes a card safe to approve.
- **Registration and tests.** Rows are handed to the store when the server starts, not registered from the store's own `init()` as 18 files did at 635f47b7b and 15 do at fdde0e705 (2.2). The seam is the server's start. `yardr server` is a command in `internal/cli` (`cli/ops.go:47`), which calls `server.Run(ctx, cfg, log)`, and `server.Run` opens the store (`server/server.go:134`). `internal/cli` already imports `internal/server` (`cli/root.go:23`), and the server cannot import the cli without a cycle, so the rows travel down that call: the cli, which imports every package that holds a row, gathers them into one list and passes it to `server.Run` as an argument, and `server.Run` gives it to the store it opened (`st.SetRows(rows)`, before the socket is served), as it already hands the store its pack fetcher (`st.SetPackFetcher`, `server.go:154`). The server itself names no row and imports no package for its rows. A row that lives in `internal/cli` beside its command is in the list's own package; a row in a package the command calls (`internal/pack`, `internal/source`) is imported by the cli as that command already imports it. Only the server's process approves or asks (both run in the store that owns the database), so no other process needs the list. A store test of the machinery registers a test row of its own on its store, and a row's tests live beside the row, with a throwaway store from `internal/storetest` and that row registered. A store built by `storetest` alone has no command rows, and its tests say so.
- **When the rows move.** After the light-engine deletions of card types, so that nothing is moved that is about to be deleted (decision 3). yardr-mzy7 deleted 13 types (7b62f0784). The move waits until no further deletion of card types is open, and section 6 puts it in that order.
- **Owner.** The core owns the ask shape and the row interface. Each command's owner owns its row.
- **Version.** The ask's JSON is on the wire for footplate (`AnswerAsk`, `ApproveAsk` and `UserAsks` are in `rpc.Calls`), so it follows the view contract's rule (3.2.7).
- **Change rule.** A new command on the card list is a row in its own package plus a line in the page's table of rows. The core does not change.
- **Stored data.** Ask notes, with `AskRun` holding the stored arguments and fingerprints, and the carry bucket. When a row's card sentences change, every open card of that command goes stale on upgrade, because approval compares `Does` and `Seen`. The cost is that each such ask must be asked again. Nothing is lost. A stored card whose arguments the new parse cannot read must be refused on approval with its reason, never carried.

#### 3.2.7. View: what web, chat, mcp and footplate read

- **Today.** `store.API` (328 methods) for every reader inside the module. `rpc.Calls` and `rpc.OperatorCalls` (55 calls) for footplate and other programs, under "on the wire for good" (`internal/rpc/api.go:35-45`), with `docs/api.md` written by `TestAPIDoc`. footplate's source names 42 of the 55, measured at footplate's origin/main af70127 (25 Nov) by listing the names of the two maps in `internal/rpc/api.go` and counting those that `git -C <footplate> grep -q -w <name> origin/main -- src` finds. Chat (`internal/chat/chat.go:40-52`) and mcp (`internal/mcp/mcp.go:369-398`) already read through narrow interfaces of their own. Web holds the whole `store.API` and also imports `dispatch`, `eta`, `limits`, `config` and `server` (`internal/web/server.go:49`, `web/sessions.go:10`, `web/health.go:8-9`).
- **Form.** RPC calls and event kinds (the socket and the listener's `/user/call/<Method>`).
- **Owner.** The core.
- **Version.** Proposed: one number for the view contract, stated by the yard. A client says the number it speaks in the `X-Yardr-Client` header that exists today ("footplate/0.1" becomes, for example, "footplate/0.4 view/3"). A call from a client that names another number is answered with a plain refusal that names the number the yard needs and tells the person to upgrade the client. The number binds the callers that name themselves in that header, and only them. yardr's own callers (the CLI, the daemon, chat, mcp, web, every `rpc.Client`) do not send `X-Yardr-Client` (`rpc/client.go:200-225`) and need no view number: they send the build's revision (`X-Yardr-Revision`, `version.Header`), and the server already holds them to one build, handing over to a new build when a call names a revision it does not run (`server/server.go:434-441`). On the unix socket, which only the yard's own user reaches, a call that names neither header is served as it is today (`rpc/api.go:11-13`), since tests and a person's own `curl` call that way. On the listener (`/user/call/<Method>`, `rpc/usere2e.go:271`), whose callers are devices and never yardr's own build, a call of one of `Calls` or `OperatorCalls` with no client header is refused with the number it needs. The edge's enrolment (`EnrolEdgeDevice`, `rpc/useredge.go:29-33`) arrives on the same path but is no view call: it is the edge link's (3.2.8) and is held by the edge's signature, not by this number. Adding a call or an argument whose zero value means the old behaviour keeps the number. Removing or re-meaning one raises it, in one release.
- **Change rule.** The view contract changes whole, as every other contract does (mail 246738), and footplate follows: "on the wire for good" in `rpc/api.go` gives way to the number. Proposed in addition: every view, the web included, reads through a named interface of the calls it uses, as chat and mcp do, and no view imports `dispatch` or `server`. A view-shaped read (`line.go` and the others in 3.1, case 3) is listed on the page as a call made for a named view, so that it can be found and later moved.
- **Stored data.** None of its own beyond device tokens, channel keys and the footplate push queue (`store/footplate.go`).

#### 3.2.8. Peer

- **Today.** `/peers/messages` and `/peers/pair/*` (`internal/yards/receiver.go:33-47`), `PeerMessage{id, kind, re, body, seq}` (`internal/store/peers.go:216`), capabilities in `PeerSays` (`peersaid.go:19`), and a per-body `v` (`PeerBeadVersion = 1`, `PeerAskVersion = 1`, sealed envelope v2), signed under RFC 9421 with ed25519 and sealed with HPKE. The rule is in `docs/peers.md`, "Two yards on different builds".
- **Form.** An HTTP protocol between two yards on different builds.
- **Owner.** The core. Peering as delivery stays in the core (decision 4).
- **Version.** Per body, as today. The norm page lists each kind and its `v`.
- **Change rule.** A shape change raises the body's `v` in one release, and the yard reads only the `v` it writes. A message at a `v` the receiver does not read is refused as `kind_unknown` is today: logged, told to the sender, never dropped silently, and named in plain words with the build it needs (`refused by the peer: <peer>'s yardr reads bead v2; it needs an upgrade`). The linked yard that is behind follows by upgrading, as with packs (mail 246738).
- **What a sender knows before it sends.** Today, only which kinds the peer understands: `PeerSays` (`peersaid.go:19-22`) carries `understands` (kind names), the build and the roster, and no version of any body. A sender learns that the peer does not read a body's `v` only from the refusal after sending: the receiver answers `version` (`RefusedVersion`, `peerbeads.go:62`) with the `v` it got and the one it knows (`known`, `peerbeads.go:1274-1279`), and the sender ends the send record as refused with the peer's reason (`peersend.go:633`, worded by `PeerRefusal`, `peermail.go:189`). That refusal is the contract's guarantee, and it holds between any two builds. Proposed beside it: `PeerSays` gains `versions`, the `v` the build reads for each kind that has one. It is a field an older peer ignores and an older sender does not send, so adding it raises no number. A sender whose peer said its versions holds a message at a `v` the peer does not read in the outbox as refused before it sends, with the same words. A peer that said no versions (an older build) is sent to, and the refusal after sending says the same.
- **Stored data.** Peers, the outbox, peer beads and asks in both directions, reads and roster: 23 store files. A message refused for its `v` stays in the outbox as given up, with its reason.

The edge link (`internal/edgelink`, frames on one WebSocket, `link.go:37-72`, with no version on any frame) has the same shape as peer. It is named here so that its norm page is written with peer's. Its far end is the edge service, which yardr also runs, so it versions its frames as one `v` on `hello` rather than per frame, and either end refuses a `v` it does not read with the version it needs.

### 3.3. Where the rules are declared

Decided (decision 1): one norm page per contract under `docs/contracts/`, each rule paired with a check the gate runs, and pointers only in AGENTS.md and the role sections. The reasons, weighed against section files spread through the packages:

1. **A contract has two sides, and a package file sits on one.** The harness contract is read by store, agents, runner, dispatch and cli today. A section file in any one of them is not where a builder in another looks. A page per contract is found from its name.
2. **The precedent works.** `docs/architecture.md` with `arch_test.go`, `docs/api.md` with `TestAPIDoc`, and `docs/pack-api.md` with `TestPackAPIDoc` are the rules in this repository that have held. The prose-only rules have not: the cli rule from p326, and the store's size.
3. **Section files multiply.** p326 split large files into many, and the store went from 59 files to 260. Rules spread through packages would follow the same path.

Section files beside the code are right for one thing: a package's own `doc.go` comment saying which contract it implements and pointing to the page. `go doc -short`, which AGENTS.md already tells builders to run first, then shows the pointer.

What the pages are:

- `docs/contracts/` holds one page per contract in 3.2 (eight, with the edge link on the peer page) and one page for the core itself, `docs/contracts/core.md`: the sorting rule of 3.1 and the growth checks of 3.4. `docs/architecture.md` folds into `core.md`, and the tests, briefs and pages that link to it are changed in the same commit.
- Each page states its rules as a numbered table: the rule, and the check that holds it (a test name, or a gate step). A rule with no check says `unchecked` in that column, so the gap is visible and countable.
- One test, beside `TestImportRules` in the root package, reads every page and fails when a rule row names a test that no `*_test.go` file in the module declares, or leaves the column empty. A row may say `unchecked`. It may not be blank. This is one new mechanism beside `TestAPIDoc` and `TestPackAPIDoc`, and it differs from them: they write generated sections from code, while a contract's rows are written by hand and only their checks' names can be held. It reads the test names with `go/parser`, as `arch_test.go` reads imports, and starts no process.
- AGENTS.md, `.yardr/depot.md` and the role sections stop restating rules and point to the page. AGENTS.md keeps its package map.

Rules that would be `unchecked` on the first day, from what 2.4 found: the cli rule ("a command parses flags, asks and prints"), the peer change rule, "a new kind is a file plus fixtures, with no Go", the harness reviewed-bytes rule until its test is written, "a view reads through a named interface", and every handle key being listed.

### 3.4. What stops growth, not only cycles

An import rule stops a cycle. It does not stop a package from growing, and 2.2 shows that it moved code into the store. Decided (decision 2): both of the following checks, on `docs/contracts/core.md`, run by the gate.

1. **A line ceiling per core package, lowered in steps.** `core.md` holds a table of core packages and a ceiling for each in non-test lines, read by the root-package test of 3.3, which counts the package's non-test Go and fails a change that takes it over its ceiling. The first numbers are set from step 2's count. The ceiling is a fixed number. It is not rewritten at every landing: a third of all commits touch the store (632 of 1,911, 2.2), and a number every landing rewrites would set them colliding on one line. It is lowered in steps, each by a bead of its own, after a move out of the core has landed. Between those beads, new core work makes room by moving how-and-what out first, and the room is the gap between the package and its ceiling. Because it counts the package, not the file, splitting a file does not help, which closes the gap `filesizes` left.
2. **A written list of what the core may not name.** `core.md` holds the list itself: the harness kinds, the providers, herdr's command words, and the word lists of yardr commands outside the core's own. It is not read from `packs/` directory names, which would also hit generic words such as `design`, `dir`, `routine` and `starter`. A test scans the non-test Go of each core package for the listed names as whole string literals and as whole parts of identifiers (so `pi` matches `"pi"`, not `pick`), skipping comments, and fails with the file and line. Today's hits are an allow-list in the test that may only shrink, as `filesizes` rows only fall, so a new hit fails from the day the check lands. A second test fails when a harness file in `harnesses/` names a kind that is not on the list, so a new kind joins the list in the commit that adds its file.

A third check, that the core may not import a how-and-what package, would be an import rule like (a). It would also push code inward the way (a) did, unless the ceiling in (1) is in place first. So the order matters: the ceiling lands before any new import rule.

The ceilings depend on what step 2 counts as core, so this design names the mechanism, and the bead that adds it comes after step 2 (section 6).

### 3.5. What a break costs

mem-y81p stands: yardr may break with a new version, removals are whole, and only a yard's stored data must survive an upgrade. mckean's mail 246738 adds that the other end of a contract follows the core, with no grace period. Per contract, what a break touches:

| contract | other end upgrades with this yard? | what the other end gets when it is behind | stored data a break must carry |
|---|---|---|---|
| harness | yes (files are in the build) | — | group args, model and kind; session handle keys |
| source | yes for packs in the build; no for packs from yardr-packs or a URL, and for a standalone declaration | the declaration is refused with its reason and the pack API it needs | stored declarations, grants, keys, call log, hook deliveries, links |
| runner | yes (internal); herdr no | herdr is refused at start with the version it needs | live sessions and their handles across the restart |
| flow and stage | yes in the build; no in a depot's `.yardr/flows/` or an outside pack | the flow is refused with its line and reason | stored flows and pointers; beads at a stage a flow drops |
| pack | no for an outside pack | refused before planning, with the API number it needs | import records, mappings, the records a pack became |
| card and ask | footplate: no | as view | open asks (go stale; asked again), the carry bucket |
| view | footplate: no | a plain refusal naming the view number the yard needs | device tokens, channel keys, the push queue |
| peer (and edge link) | no | the message refused, in plain words, with the build it needs | peers, outbox, peer beads and asks, roster |

For every row the break is whole and in one release. The cost falls on the other end: an outside pack cannot be applied, footplate cannot read the yard, and a linked yard's messages of that kind stop, each until it is upgraded, and each with a refusal that says so rather than a silent failure. What the core must still carry is the yard's stored data in the right-hand column, and each break needs a migration for it, which the existing `migrate_NNN_*.go` registry carries.

## 4. Left out

- **Moving any file, and any new repository.** The bead excludes them, and step 2's number decides their shape.
- **Choosing between a new core and cutting in place.** That is step 3.
- **A store split by area.** p326 decided against it (its question 4). The sorting here is core against how-and-what, which is a different line, and the store stays one package on the core side.
- **A grace period or a kept old form for any contract.** mail 246738 rules it out. The first draft proposed a new name or `v` beside the old one, removed on a stated date, for packs, view and peer. That is dropped.
- **tpb2's harness pack, rebuilt.** The harness contract in 3.2.1 replaces its form (decision 4). Its security rule carries over (3.2.1, "Reviewed bytes").
- **Peering out of the core.** Peer records are 11,387 lines of the store that mail and beads between yards rely on today. Taking them out would be a design of its own (decision 4).
- **A kept table of every file's side, and a gate mode that reports it.** The first draft's step 2 built `docs/contracts/sides.tsv` with a coverage test and a `sides` mode in `.yardr/test`. Step 2 is one measuring bead instead (decision 5), and a table is built later only if step 3 chooses cutting in place.
- **Rewriting the briefs' role sections.** Pointing them at the pages is part of the beads after step 2, once the pages exist.

## 5. Decisions and open questions

mckean decided these on 24 Nov 2026, each with the first option offered:

1. **Norm pages** (ask 30918): `docs/contracts/<name>.md`, one per contract, with `docs/architecture.md` folded into `docs/contracts/core.md` (3.3).
2. **Growth** (ask 30920): both checks, the line ceiling per core package lowered in steps by a bead of its own rather than at every landing, and the may-not-name list written out rather than read from folder names (3.4).
3. **Run cards** (ask 30925): the core keeps ask, approve, staleness and carry and the row interface, and the rows move beside their commands, after the light-engine deletions (3.2.6, section 6).
4. **The edge** (ask 30922): the harness as an interface in the core plus files and fixtures outside, with no per-kind Go and tpb2's pack form dropped; peering stays in the core with its wire as a contract (3.2.1, 3.2.8).
5. **Step 2** (ask 30923): one bead, the store sorted file by file and every other package as one row, the result in the bead's note, no table kept and no test (section 6).

And by mail 246738: no grace period for any contract's other end; an older pack, footplate or linked yard is refused with a reason that names the version it needs (3.2, 3.5).

And on 25 Nov 2026 (ask 31498, sent back to draft with these words):

6. **A yard's own harness file**: "A yard may carry its own harness file for matchers and models, never for what starts a program or answers its dialogs." This is the option the earlier draft offered as B, against only the files in the build (A) and every field through an approved run card (C) (3.2.1).

No question is open. The critique of 25 Nov 20:13 left four points for this round, and each is answered in place: where the card rows are handed to the store (3.2.6, "Registration and tests"), which of a row's functions are bound to the write and how the others get their inputs (3.2.6, "The write a row runs in"), how the sorting rule places files that are not product Go (3.1, the end), and the store's growth net of every move, the eta move outward included (2.2).

## 6. The beads it would become

Step 2 in full, as one bead (decision 5). It only measures: it moves no code, changes no behaviour, and keeps no file.

**Count the core of yardr against the sorting rule.** Goal: one number for step 3, the non-test lines of Go that are core, how-and-what, library and mixed, measured at one named commit of main. internal/store is sorted file by file, by 3.1's two questions and its seven named cases; a `mixed` file is split by function, with the functions on each side named and their lines counted. Every other package under `internal/`, and `main.go`, is one row with one side, or `mixed` with an estimate of each side's lines and the reason. The note also counts the hits the may-not-name list of 3.4 would find in the core today: the harness kinds, the providers and herdr's command words, by package. It also counts the width `store.Tx` would need (3.2.6): the distinct store functions and buckets that the card rows' functions reach, at the same commit. Where to look: section 2.2's file families and its command; `go doc -short ./internal/store` for the exported names per file; AGENTS.md's package map and each package's `go doc -short` for the rest; 3.1 for the rule. Done when: the bead's note holds the table (store files one per row, other packages one per row), the sums per side, the may-not-name hits, the width of `store.Tx`, the commit it was measured at and every command it used, and the sum of the sides equals the measure's product total at that commit. No file in the repository changes. It depends on nothing but this design's approval.

**After step 2**, in one paragraph because their shape depends on that number. If the core is small, say under a third of the product lines, step 3 leans toward a new core beside the old one: the norm pages are written first, then the core is built against them and the how-and-what is moved across contract by contract, with the ceiling and may-not-name checks in the new core from its first commit. If it is large, cutting in place is cheaper, and a table of each file's side is built then, as the list of what to move. The order would be: the norm pages with `docs/architecture.md` folded into `core.md` and the page-reading test (3.3); then the line ceilings at step 2's numbers and the written may-not-name list with today's hits allowed (3.4); then one bead per contract that moves its how-and-what out of the core, each followed by a bead of its own that lowers the ceiling it freed. Among those, the harness knowledge into files and fixtures (3.2.1), the store's Google code and view-shaped reads, and the dispatch housekeeping can go in any order; the run-card rows move last, once no deletion of card types is open, so that nothing is moved that is about to be deleted (3.2.6). Either way, the beads are filed after step 3's decision, not from this document.
