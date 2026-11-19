# Setting up Slack

The pack wires Slack into the yard as the declared source `slack`: the
messages of a bot's channels, groups and one-to-one direct messages in a
workspace, heard through the Events API and answered in their threads
with `yardr reply`. A mention in a channel the bot is in lands in the
yardmaster's inbox; every other message there is stored and read back
with `yardr thread show`, and nudges nothing. A one-to-one direct message
lands whole, mentioned or not. What lands is answered where it stood, in its thread. Group
direct messages are untested: whether Slack delivers a mention in one
as `app_mention` is checked in a live trial, and until then this walk
promises only channel mentions and one-to-one direct messages.

The yard side is cards the whole way: a crew member asks, and the
operator answers on the card, in footplate or in `yardr chat` (`ctrl+o`),
where a secret field shows dots. What is not a card is named as the
operator's: Slack's pages in a browser, the invitation in Slack itself,
and the networking that reaches the yard. The steps use `mckean` as the
operator's user name; replace it with the operator's user name in this
yard if it differs.

1. **The app, from the pack's manifest, in a browser.** At
   api.slack.com/apps, Create New App, From a manifest: paste
   `packs/slack/app-manifest.yaml` from a checkout of yardr (or from the
   pack as the yard installed it). The manifest names the bot user and
   the scopes (`app_mentions:read`, `channels:history`, `channels:join`,
   `channels:read`, `groups:history`, `groups:read`, `im:history`,
   `mpim:history`, `chat:write` — the four histories are bot scopes, and
   they are what `conversations.replies` reads a thread back with on the
   bot token in each kind of conversation; `channels:join` lets the
   `join` call put the bot in a public channel, and `channels:read` and
   `groups:read` let the `channels` call list the ones it is not in),
   and carries no event subscriptions and no Request URL. A
   manifest that names events without a Request URL does not validate,
   so the events cannot stand in the document the app is created from
   while its hook address is not yet known; they are added in step 6,
   once the hook stands and Slack can verify its address. Install the
   app to the workspace. Two values Slack shows are copied from there
   into cards below: the signing secret (Basic Information) and, after
   install, the bot token `xoxb-…` (OAuth & Permissions). Making the app
   and installing it are the operator's in Slack's pages; no terminal is
   involved, and no card can do them, since the yard holds no Slack
   credential yet by which it could act.
2. **Apply the pack, on a card.** A crew member proposes it and the
   operator approves:
   ```sh
   yardr bead ask <bead> "Add Slack to the yard?" --run "pack apply slack" --context "The yard hears the workspace's mentions and direct messages through it."
   ```
   The card shows the declaration whole, as the apply card renders every
   field of it, the two scripts with their checksums among them.
3. **The bot token, on a secret card.**
   ```sh
   yardr bead ask <bead> "The Slack bot token" --secret slack --context "OAuth & Permissions in the Slack app, after install: xoxb-…"
   ```
   Typed unseen, written by the server to the source's env file as
   `SLACK_BOT_TOKEN`; in no note, as every secret ask.
4. **Whose token it is.**
   ```sh
   yardr source check slack
   ```
   The yardmaster runs it where it may make the read call, else mails
   the operator the command. It prints the selected fields of
   `auth.test`, one per line under the status line:
   ```text
   slack: check call whoami (POST /auth.test): 200 in …ms, … bytes
   slack: user_id: U…
   slack: team_id: T…
   slack: bot_id: B…
   ```
   The first is the id step 7 sets, a token Slack refuses fails the
   check here rather than at the first mention, and a check that prints
   no `user_id` line has not shown whose token it holds: setup stops
   there rather than guessing an id from elsewhere.
5. **The public hook address, on a card if it is not already set.**
   ```sh
   yardr bead ask <bead> "Where the yard is reached for webhooks" --run "settings set hook_public_url https://<his host>/hooks" --context "Slack posts the workspace's events to the hook's URL under it."
   ```
   The URL of the slack hook is then
   `https://<his host>/hooks/slack`. The address reaching the yard — a
   tunnel or funnel in front of `hook_addr` — is networking outside
   yardr, the operator's, and is the one piece of plumbing no card
   carries.
6. **The signing secret and the hook, on two cards, then the events in
   the browser.** First the secret:
   ```sh
   yardr bead ask <bead> "Slack's signing secret" --secret-hook slack --source slack --context "Basic Information in the Slack app."
   ```
   then the hook:
   ```sh
   yardr bead ask <bead> "Set up the Slack webhook?" --run "hook add slack --source slack --unknown unknown" --context "The yard takes the workspace's mentions and direct messages at POST /hooks/slack."
   ```
   `--unknown unknown` is the decided word, for the whole workspace: a
   sender no user maps is dropped and offers the operator a mapping
   card, until they are mapped or ignored. Unlike telegram there is no
   registration call for the yard to make on approval: Slack takes its
   Request URL only in its own configuration, so approving the hook
   stands the endpoint up and nothing is sent anywhere. Then, in the
   browser, the operator opens the app's Event Subscriptions, turns it
   on, pastes the hook's URL (`https://<his host>/hooks/slack`) as the
   Request URL, and subscribes to the five bot events, exactly these
   five:
   ```text
   app_mention
   message.im
   message.channels
   message.groups
   message.mpim
   ```
   The yard hears every message in a channel or group the bot is in,
   mentioned or not, and stores it; only a mention nudges the yardmaster.
   A channel message that mentions the bot arrives twice, as its
   `message.channels` (stored, nudging nothing) and as its `app_mention`
   (heard once), and the pack's normalize keeps the mention off the
   first, so the bot is woken once. A group direct message is the same:
   stored from `message.mpim`, woken by its mention alone. A one-to-one
   direct message is heard whole. Slack sends a
   `url_verification` challenge to the URL when it is saved; the yard
   answers it from the hook just made, and the app shows verified. This
   is the visit the manifest of step 1 deliberately left undone: only
   now does the app have a reachable Request URL to name beside its
   events, so the subscriptions validate. This paste is a step Slack
   leaves no API for; it is in a browser, not a terminal, and the
   browser is the operator's.
7. **The bot's identity, on a card.** With the `user_id` and the
   `bot_id` step 4 printed, so `@bot` is a mention of the yardmaster:
   ```sh
   yardr bead ask <bead> "Whose mention the bot's is" --run "crew identity yardmaster --source slack --id <user_id from step 4> --key <bot_id from step 4>" --context "The user_id and bot_id yardr source check slack printed in step 4."
   ```
   The key is the bot id because the echo of the bot's own post may
   carry only `bot_id`: with both set the yard drops its own posts
   before hearing, under any Unknown word, and offers no mapping card
   for them.
8. **The yardmaster's own credential, for answering, on a card.**
   `yardr reply` speaks with the answering crew member's copy of the
   bot token:
   ```sh
   yardr bead ask <bead> "The token it answers with" --secret slack --for-crew yardmaster --context "OAuth & Permissions in the Slack app, after install: xoxb-…"
   ```
   The yard's server writes it into the crew member's env file, or with
   none set into `~/.yardr/crew/yardmaster.env`, which is then set as
   `yardr crew set` sets it. The running session does not see the new
   value: it takes effect when it next starts, so restart it:
   ```sh
   yardr crew restart yardmaster
   ```
   Rotation rewrites both files, the source's and the crew member's.
9. **The subscription, and an invitation.** The yardmaster subscribes
   itself from its pane — an agent step, not an operator terminal —
   to mentions, and to one-to-one direct messages whole:
   ```sh
   yardr subscribe yardmaster slack --scope mentions --where type=message
   ```
   ```sh
   yardr subscribe yardmaster slack --scope all --where type=message --where room=dm
   ```
   `mentions`, not telegram's `all`: a workspace is many channels of
   other people's talk, and the bot is woken by being addressed, not
   by everything said where it stands; what is said where it stands is
   still stored, and reads back with `yardr thread show`. The bot hears a
   channel only once it is in it: the `channels` call lists the public
   channels with `is_member` true or false, and the `join` call, one
   channel id per call (`yardr call slack join channel=C…`), puts it in
   each one that is false; a private channel
   needs `/invite @<bot>` in Slack, since the bot cannot join one itself.
   Without either, Slack sends the channel nothing. A first mention is
   then the test: it lands in the yardmaster's inbox as
   ```text
   5210 10-17 18:02  hook (mentioned) slack message create by ada (U012AB3CD): <@U…> can the shed land on Monday?
   ```
   and `yardr reply 5210 -` answers in that thread, with the words on stdin
   (here: Noted; I will post here when it lands.). The thread is read back
   with `yardr thread show 5210` on this channel delivery too — the bot
   belongs to the channel it was invited into, and its token holds
   `channels:history` — and `yardr hook show 5210` holds the delivery itself
   meanwhile.
   Channel broadcast remains unavailable with the shipped manifest's scopes.
   Before posting, `--broadcast` uses
   [`conversations.info`](https://docs.slack.dev/reference/methods/conversations.info/)
   to confirm the conversation is a channel. The bot token must already have
   `channels:read` for a public channel or `groups:read` for a private channel;
   add the needed scopes under OAuth & Permissions in the Slack app, then
   reinstall the app to the workspace to authorize them. For an existing
   installation, adding scopes alone does not update its authorization.
   If Slack issues a replacement bot token, repeat steps 3 and 8 to update
   both copies and restart the answering crew member. Once authorized, use
   `yardr reply 5210 --broadcast -` with the answer on stdin for a
   channel-thread answer that should also appear in the channel. Missing
   access or an unconfirmed channel refuses the reply before posting; direct
   and group direct messages are refused too.
   To address the person explicitly, include Slack's mention syntax in the
   answer, for example `<@U012AB3CD> Noted; I will post here when it lands.`
   Broadcast does not add a mention to the supplied text.
   The companion check in the same walk: a one-to-one direct message
   mentioning nobody still lands whole by the `room=dm` subscription.
   A channel message mentioning nobody is stored under its channel and
   nudges nothing: `yardr thread show` on its delivery reads the thread
   back, and no inbox line comes of it. A group direct message is the same
   under `message.mpim`, and a group mention is not tried here: group
   direct messages stay untested until the live trial.

Who is heard is what the subscriptions above carry, and who each
speaker is. The pack hears mapped members as themselves: a delivery
whose sender is a user's `slack` identity arrives named as them, and a
subscription may filter with `--where speaker=<user>`. An unmapped
sender follows the hook's Unknown policy: see
[identity hearing](../../docs/integrations.md#what-the-service-sends-in)
for held deliveries, their inbox notices, the operator's mapping card and
its answers. `yardr source show slack` and `yardr source ignore slack`
list the ignored ids, and `yardr source ignore slack --rm <id>` hears
them again. Three limits, where you meet them: a reply run again
after its answer was lost posts twice, since the service takes no
idempotency key; `yardr thread show` on a delivery whose conversation
the bot has left, or whose kind its token has no history scope for,
fails with Slack's reason rather than showing an empty thread; and
`yardr reply --to` stands with `thread show`, since it finds the
message to answer under by listing the thread. Edits and deletes in
direct messages arrive under `message.im` and are ignored, so a mention
edited after it was heard stands as it was heard. The bot's own posts
name it as actor where Slack sends them back, and never reach its
inbox: a delivery whose sender is the crew member's `slack` identity,
by its id or its key, is dropped before hearing, with no mapping card,
under any Unknown word.
