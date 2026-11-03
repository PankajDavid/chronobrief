# ChronoBrief TTL Frontier Simulation: Truth-Based Caching vs. Time-Based TTL

**Pitch Line**: *"Generate once, reuse when the evidence is unchanged, regenerate when the retrieved facts move."*

> [!NOTE]
> **Simulation (250 synthetic lookups, assumed news arrivals)**: Queries and news arrival intervals are synthetically generated (`math/rand` seed `20261102`), not recorded production traffic. This simulation illustrates the trade-off between time-based TTL expiration and evidence-based freshness over a 48-hour timeline across 5 tracked companies.

## Simulation Results Matrix (250 Synthetic Lookups)

| Strategy | Cache Decision Rule | Hit Rate | Stale Serves | Stale Serve Rate | False Misses | Tokens Saved | Cost Saved |
|:---|:---|:---:|:---:|:---:|:---:|:---:|:---:|
| **TTL-5m** | Standard 5-minute TTL (short TTL) | **6.4%** | `0` | **0.0%** | 222 | 83,200 | $0.0312 |
| **TTL-1h** | Standard 1-hour TTL | **52.4%** | `5` | **2.0%** | 107 | 681,200 | $0.2555 |
| **TTL-24h** | Standard 24-hour TTL | **96.0%** | `46` | **18.4%** | 5 | 1,248,000 | $0.4680 |
| **freshcache** | ChronoBrief ExactSet + 24h ceiling | **93.2%** | `0` | **0.0%** | 5 | 1,211,600 | $0.4543 |

## Visual Pareto Frontier: Hit Rate vs. Stale-Serve Risk

Every marker below is placed by code from the table rows above (linear axes). Stale serves and false misses are scored by the same independent oracle for every policy, freshcache included.

```text
Hit rate (%)
100 |                                                       C
    |D
 80 |
    |
 60 |
    |      B
 40 |
    |
 20 |
    |A
  0 |
    +-------------------------------------------------------------> Stale-serve rate (%)
     0              5             10             15             20

A = TTL-5m     hit   6.4% | stale   0.0% (0) | false misses 222
B = TTL-1h     hit  52.4% | stale   2.0% (5) | false misses 107
C = TTL-24h    hit  96.0% | stale  18.4% (46) | false misses 5
D = freshcache hit  93.2% | stale   0.0% (0) | false misses 5
* = two policies in the same cell
```

## Key Findings (Simulation)

1. **freshcache**: 0 stale serves (0.0%) at a 93.2% hit rate over 250 synthetic lookups. This is an empirical result of this simulation; the formal guarantee is tested separately by `TestNoStaleServeInvariant`.
2. **TTL-24h**: 96.0% hit rate, but 46 stale serves (18.4% of lookups) after simulated news arrived.
3. **TTL-5m**: no stale-serve risk but only 6.4% hit rate; 222 of 250 lookups (88.8%) recomputed although the evidence had not changed.

These numbers come from synthetic traffic and assumed news arrivals. They illustrate the trade-off; they are not a measurement of production traffic.
