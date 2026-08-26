# D4 — titan-microservices-mesh · Detailed Completion Plan

*Source evidence: reports/titan-microservices-mesh.md (statically verified 2026-08-24) · nodes D4-1..D4-DEPLOY
from deployment_ready_minimum_diff_plan.md · intent from project_intent_analysis.md §D4.*

---

## 0. Identity

| Field | Value |
|---|---|
| Local path | `C:\Users\thela\Downloads\projects context\Magnificent-Seven\titan-microservices-mesh` |
| Remote | `github.com/nithin12342/titan-microservices-mesh` |
| Branch | `master` (synced 2026-08-25; test-suite commit `90a7c55` already pushed) |
| Intent | E-commerce checkout correctness across 3 independent services via circuit breakers + sagas, rollout risk managed by Istio canary routing |
| Input → Output | POST /api/v1/orders JSON (items, qty, payment, idempotency key) → breaker-wrapped reserve+charge → CONFIRMED/REJECTED order + mesh canary telemetry |
| Deploy target | killercoda free k8s first (helm install + curl /health), then ACA Consumption on Azure Students; images from GHCR; destroy after demo |
| Verdict at study time | 🟡 Partial: 3 real Go services (NOT "6 across 5 languages"); order-service unbuildable (no go.mod) with stubbed core calls + broken ID generator; Istio YAML parse error; Terraform mostly invented schema |
| Estimated effort | ~1 day + playground session |

## 1. Verified defect inventory

| # | Defect | Exact location | Reproduced? |
|---|---|---|---|
| B1 | No go.mod/go.sum → `go build` impossible as committed | `services/order-service/` (inventory+payment HAVE go.mod) | ✅ inventory |
| B2 | Core business calls are comment stubs returning true — every order auto-confirms | `services/order-service/main.go:107-133` (`checkInventory`/`processPayment`) | ✅ full read |
| B3 | `randomString(n)` time-seeded per byte → all chars identical, IDs non-unique | `main.go:139-146` | ✅ read |
| B4 | GetOrder returns fabricated order regardless of ID (no store) | `main.go:88-105` | ✅ read |
| B5 | YAML syntax error in retries block → whole config fails to load | `mesh/istio-config/virtual-service.yaml` (payment VS) | ✅ structural read |
| B6 | VirtualServices route to subsets canary/stable with NO DestinationRule defining them → traffic black-hole; user-service VS for nonexistent service | same file | ✅ inspection |
| B7 | Invalid azurerm args: `retention_days`, resource_group_name=location copies, top-level container/scale/ingress blocks vs real `template{}` layout, `@@PASSWORD@@` literal secrets, fqdn output reads undefined `.configuration.ingress`, zone_redundant on Standard Service Bus | `infrastructure/container-apps/terraform/main.tf:41,101,189,237,98-183,171,176,224,272,316` | ✅ arg-by-arg schema cross-check |
| B8 | README promises compose/skaffold/user/analytics/notification services — none exist | `README.md:13-15` | ✅ tree diff |

## 2. Node plan

### P0 RUN-FIX

**Node D4-1 — all three services build**
```
GOAL      : go build ./... exits 0 in all three service dirs.
LOCATION  : NEW services/order-service/go.mod (+go.sum)
MIN-DIFF  : go mod init github.com/nithin12342/titan/order-service && go mod tidy
            (Go 1.24.2 already installed locally ✓)
VERIFY    : cd services/{order,inventor,y,payment}-service && go build ./...
EXPECTED  : exit 0 ×3. Artifact: verification/d4-1_go_build.txt
SIBLINGS  : none yet
```

**Node D4-2 — unique IDs**
```
GOAL      : 10k generated IDs contain zero duplicates.
LOCATION  : services/order-service/main.go:139-146
MIN-DIFF  : replace time-seeded byte loop with crypto/rand read
NEW TEST  : main_test.go::TestRandomStringUniqueness (10k IDs)
VERIFY    : go test ./... in order-service
EXPECTED  : pass. Artifact: verification/d4-2_ids.txt
```

**Node D4-3 — order flow is real**
```
GOAL      : POST /orders actually calls inventory/payment over HTTP inside the existing
            breaker wrapper; unreachable dependency → 400 not silent confirm.
LOCATION  : main.go:107-133 (+ :88-105 GetOrder reads from in-memory store keyed by created orders)
MIN-DIFF  : http.Post to env-injected INVENTORY_URL/PAYMENT_URL; keep gobreaker wrapper;
            wire GetOrder to store
NEW TEST  : integration: unreachable inventory URL → POST /orders returns 400
VERIFY    : go test ./... + local run curl transcript
EXPECTED  : breaker path proven live. Artifact: verification/d4-3_breaker.txt
TAG       : ckpt-runs   (sibling re-runs: D4-1 build, D4-2 ids)
```

### P1 VERIFY

**Node D4-4 — Istio config parses & routes somewhere**
```
GOAL      : mesh config loads and every referenced subset has a defining DestinationRule.
LOCATION  : mesh/istio-config/virtual-service.yaml (fix retries indentation; DELETE
            user-service VirtualService) + NEW destination-rule.yaml (one DR per host:
            stable+canary subsets)
MIN-DIFF  : indentation fix + one new small file + one deletion
VERIFY    : yq/istioctl validate or kubectl apply --dry-run=client
EXPECTED  : clean parse. Artifact: verification/d4-4_istio.txt
SIBLINGS  : D4-1..3 gates re-run first
```

**Node D4-5 — Terraform validates**
```
GOAL      : terraform validate exit 0 on the Container Apps stack.
LOCATION  : infrastructure/container-apps/terraform/main.tf:41 (retention_in_days),
            :101,189,237 (rg name refs), :98-183 (container/ingress/scale under template{}),
            :171,176,224,272 (secrets from TF vars, not @@…@@), drop Service Bus
            zone_redundant, :316 fix fqdn output path
MIN-DIFF  : schema restructure per azurerm 3.x container app layout
VERIFY    : terraform init && terraform validate
EXPECTED  : exit 0. Artifact: verification/d4-5_tf_validate.txt
```

**Node D4-6 — chart renders**
```
GOAL      : helm lint passes; template renders Deployment+Service+HPA.
LOCATION  : charts/order-service (verify-only node; fix whatever lint flags)
VERIFY    : helm lint charts/order-service && helm template charts/order-service
EXPECTED  : clean render (helm portable install when batch starts). Artifact: verification/d4-6_helm.txt
TAG       : ckpt-tested   (after CI workflow green: go build/test + helm lint/template +
            istio yaml parse + terraform validate)
```

### P3 DEPLOY

**Node D4-DEPLOY — live endpoint, destroyed same hour**
```
STAGE A (free): killercoda k8s — helm install order-service chart, curl /health 200.
STAGE B (capped): ACA Consumption via student SP; images pushed to GHCR (free);
    single env, budget alert $5, terraform destroy immediately after proof.
VERIFY    : public /health 200 screenshot/transcript → verification/d4_deploy_proof/
PROOF     : tag ckpt-deployed. README descoped to "3 reference Go services + mesh"
            (compose/skaffold/phantom services → ROADMAP).
ROADMAP    : payment PSP integration · mTLS peer-auth verification on playground ·
            charts for inventory/payment or umbrella chart · second reference service if desired
```

## 3. Out of scope (ROADMAP)

Rust/Java/Node/Python services (never existed) · docker-compose.yml/skaffold.yaml · real PSP ·
AKS dedicated cluster (ACA Consumption chosen for cost).

## 4. Execution contract

POST-FIX scope check per node · sibling gates re-run before tags · evidence committed atomically ·
tags pushed same day · toolchain: Go ✓ present; terraform + helm portable installs at batch start;
killercoda account for P3 stage A.

## 5. P4 — PRODUCTION READINESS DELTA (target: `prod-ready` tag, L4)

Current level after P3 ≈ L3 (demo env destroyed). L4 requires the deployed slice to meet all gates.
| Cat | Gap | Node | VERIFY artifact |
|---|---|---|---|
| G1 | Breaker thresholds untested under load; no graceful-degradation proof at scale | D4-P4a: load test (hey/vegeta) against order flow with payment dependency degraded → breakers open, orders rejected fast (<timeout), no goroutine leak | load transcript + breaker metrics |
| G2 | Images never scanned; secrets path must be Key Vault refs end-to-end | D4-P4b: trivy image scan in CI (fail on CRITICAL); KV-ref secret wiring re-verified post-deploy | scan log + config dump |
| G3 | GORM auto-migrate vs real migrations unresolved; no backup story | D4-P4c: explicit migration step in deploy + Postgres backup schedule documented/proven once on playground PG | migration log + restore receipt |
| G5 | /health exists but /ready semantics + request-ID logging missing | D4-P4d: /ready reflects DB+dependency reachability; X-Request-ID propagated across 3 services | header echo transcript |
| G6/G8 | Canary rollback procedure written and rehearsed once | D4-P4e: VirtualService weight flip 10→0 rollback drill in playground | drill transcript |

TRACK=product.

### P4 audit addendum (production-readiness pass)
| Cat | Gap found in audit | Node | VERIFY artifact |
|---|---|---|---|
| G7 | Capacity numbers folded into G1 load test but saturation ceiling unstated separately | explicit line in RUNBOOK: max sustainable rps from D4-P4a run + degradation stance | runbook line (no new node) |
| UB | Universal Baseline UB1-UB6 applies | D4-P4f: LICENSE, gitleaks job (Go tree), govulncheck as UB3 scanner, uptime monitor on redeployed /health, README badge row | per-UB artifacts |

