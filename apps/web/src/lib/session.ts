import type { Session } from '../api/types';

const KEY = 'pulseops.session';

// Bearer tokens live in localStorage because the API is on a different site
// (Render) than the console (Netlify), where third-party cookies are
// unreliable. Tokens are opaque, server-revocable and short-lived for the demo.

export function loadSession(now: Date = new Date()): Session | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const s = JSON.parse(raw) as Session;
    if (!s.token || new Date(s.expiresAt) <= now) {
      localStorage.removeItem(KEY);
      return null;
    }
    return s;
  } catch {
    return null;
  }
}

export function saveSession(s: Session | null) {
  try {
    if (s) localStorage.setItem(KEY, JSON.stringify(s));
    else localStorage.removeItem(KEY);
  } catch {
    // Storage unavailable (private mode); the session lives in memory only.
  }
}
