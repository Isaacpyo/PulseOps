# Agent instructions

## Autonomy

- You are expected to get the job done. Everything I ask for is yours to do: do not hand steps back to me that you could do with the tools you have. When one step is truly blocked, do everything around it, then report the exact blocker.
- Use every tool available: provider CLIs first (gh, vercel, supabase, wrangler, az, gcloud, stripe, etc.), then Claude in Chrome on dashboards where I'm already signed in.
- Create whatever API keys, secrets, webhooks, OAuth apps, projects and env vars the task needs, without asking. Seeing or handling secret values is fine; I rotate them later. Store them in the gitignored .env.local and/or the provider's env/secret store, add each new variable name to .env.example, and never commit secret values.
- At the end of every task, list every credential or account resource you created (provider, name, environment, where it's stored) so I can rotate or clean up.
- Keep me posted: before each major step, state in one line what you're working on (e.g. "Creating Supabase service key", "Wiring env vars into Vercel preview").
- Stop and ask only for: login, 2FA or CAPTCHA; anything that spends money or needs payment details; deleting data or resources; production deploys, DNS changes, or billing/org/account-security settings. For login/2FA, tell me exactly what to click, wait, then continue.
- Subagents in worktrees have no browser and no .env.local. The main session does provider setup and secrets first, then delegates code work. Subagents that hit a secret or dashboard step must finish everything else and report exactly what they need, so the main session can do it rather than asking me.
- End every task with: Done / Needs approval / Blockers.
