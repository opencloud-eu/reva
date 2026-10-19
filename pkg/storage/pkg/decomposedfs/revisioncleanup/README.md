# Revision cleanup

Every change to a file keeps the previous content as a revision (version).
Without a cleanup, revisions accumulate forever and use up storage.
The revision cleanup deletes old revisions according to a configurable strategy, whenever a file gets a new revision.

It works for the `decomposed`, `decomposeds3` and `posix` storage drivers.
With posix it also covers the revisions created from changes made directly on disk (`enable_fs_revisions`).
For how those drivers are structured, see [`../README.md`](../README.md); for implementation details, see the package documentation in `cleanup.go`.

## Strategies

A strategy is a comma separated list of up to three parts.
Each `X` is either

- a plain number, which is a **number of revisions**, always counting from the newest one, or
- a number with a unit suffix, which is a **duration**: `w` weeks, `d` days, `h` hours, `m` minutes, `s` seconds.
  Durations in days or weeks can use an uppercase unit, `D` or `W`, to count from midnight (server time) instead of from now:
  `1d` means the last 24 hours, `1D` means since yesterday 00:00, `0D` since today 00:00.

| Strategy | Meaning | Example |
|---|---|---|
| `auto` | Thin out revisions the older they get (see below) | `auto` |
| `X` | Delete everything beyond `X`, no thinning | `10` keeps the 10 newest revisions, `30d` deletes revisions older than 30 days, `0` deletes all revisions |
| `X, auto` | Keep everything within `X`, thin out the older revisions | `14d, auto` |
| `auto, X` | Delete everything beyond `X`, thin out the rest | `auto, 90d`, `auto, 50` |
| `X1, X2` | Keep everything within `X1`, delete everything beyond `X2`; one must be a number and the other a duration | `5, 30d` deletes revisions older than 30 days but always keeps the 5 newest; `7d, 10` keeps at most 10 revisions but never deletes revisions younger than 7 days |
| `X1, auto, X2` | Keep everything within `X1`, delete everything beyond `X2`, thin out in between | `7d, auto, 90d`, `2, auto, 50`, `1D, auto, 30d` |
| `disabled` | Never delete revisions (same as empty) | `disabled` |

Rules worth knowing:

- **`auto` thins out revisions** by keeping one revision per time slot. The slots get longer the older the revisions are:

  | Age of the revision | Kept |
  |---|---|
  | last minute | one per 10 seconds |
  | last hour | one per minute |
  | last 24 hours | one per hour |
  | last 30 days | one per day |
  | older | one per week |

- **A number of revisions is a total**, the protected revisions included: `7d, 10` keeps 10 revisions in total, not 10 on top of the ones from the last 7 days. Revisions within the protected range are never deleted, though, even if there are more of them.
- **In `auto, N`, the number `N` applies first, then `auto` thins out what is left.** So `auto, 10` keeps *at most* 10 revisions, usually fewer, and sometimes fewer than `auto` alone: if the 10 newest revisions were all saved within the same minute yesterday, `auto, 10` keeps a single revision, while `auto` keeps one of those plus one per day before. It does *not* mean "thin out first, then keep the 10 newest of what is left". The same goes for a number in `X1, auto, N`.
- **Two values of the same kind are rejected** in `X1, X2`, because one of them would have no effect: `5, 10` is the same as `10`, and `7d, 30d` is the same as `30d`. The error message names the equivalent single value.
- **In `X1, auto, X2` with two values of the same kind, `X1` must be smaller than `X2`**, otherwise `auto` would never apply.
- **`0` deletes all revisions.** Unlike `disable_versioning`, which only stops creating new revisions, this also removes the ones that have accumulated, over time.
