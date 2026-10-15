# Design Critique: Blind Box Page

**Target:** `src/features/wallet/blind-box-page.tsx` + `blind-box-view.tsx` + `blind-box-dialogs.tsx` + `blind-box-card.tsx`  
**Date:** 2026-06-24  
**Register:** Product (LLM gateway with gamified quota purchase)  
**Users:** Mixed — technical users (API quota management) + general consumers (casual lottery participation)

---

## Design Health Score

| Heuristic | Score | Notes |
|-----------|-------|-------|
| **Visibility of system status** | 4/4 | Payment polling (2s intervals), order status tracking, pity progress bars, real-time availability counters — all states visible |
| **Match with real world** | 3/4 | "盲盒" (blind box) culturally familiar in CN, purchase→draw→reward flow intuitive, but "保底" (pity) metaphor assumes lottery knowledge |
| **User control and freedom** | 2/4 | **P0 issue**: Payment dialog locks during pending (cannot close), no cancel or escape path. Otherwise good undo/redo in quantity selection |
| **Consistency and standards** | 4/4 | Follows project design system (app-subtle-panel, OKLCH colors, Public Sans, semantic states), consistent with wallet workspace shell |
| **Error prevention** | 3/4 | Good: disabled states, min/max validation, payment method required. Missing: no confirmation for large purchases, no spending limit warnings |
| **Recognition over recall** | 3/4 | Good: visible prize pool, recent results list, payment method icons. Weak: first-purchase guarantee buried in small panel, pity mechanics not explained upfront |
| **Flexibility and efficiency** | 3/4 | Quick quantity chips (1/3/5/10) + custom input, preset payment methods. Missing: keyboard shortcuts, bulk operations for power users |
| **Aesthetic and minimalist** | 2/4 | **P1 issue**: Hero section overloads with 4 metrics + 3 buttons + guarantee panel (violates 7±2 chunking). Prize dialog is a flat list with no hierarchy |
| **Help users with errors** | 2/4 | Payment states clear (pending/success/fail with icons + color), but no actionable recovery steps when payment fails, no retry mechanism in UI |
| **Help and documentation** | 3/4 | Prize pool dialog explains probabilities, pity mechanism documented in collapsible section. Missing: first-time user onboarding, tooltip glossary for "保底"/"盲盒" |

**Overall Score:** 29/40 (72.5%)

---

## Cognitive Load Assessment

**Passed (4/7):**
- ✓ Primary action clear (立即购买 button, prominent CTA)
- ✓ Information hierarchy established (hero → purchase card → results)
- ✓ Scan patterns respected (F-pattern in hero metrics, Z-pattern in purchase flow)
- ✓ Visual anchors present (Gift icon + gradient for blind box identity, amber accents for first-purchase)

**Failed (3/7):**
- ✗ **Chunking violated**: Hero section presents 8 interactive elements simultaneously (4 metrics + 3 buttons + 1 guarantee panel) — exceeds 7±2 working memory limit
- ✗ **Progressive disclosure weak**: All pity mechanics, probabilities, and edge cases exposed upfront instead of revealing on-demand
- ✗ **Context switching cost high**: Payment dialog forces mode switch (blocking modal during polling) with no status visibility in parent context

---

## Anti-Patterns Verdict

**AI Slop Check:** ❌ NO  
This is intentional, contextual design avoiding generic templates:
- Avoids saturated cream/sand backgrounds (uses semantic `bg-background` from project tokens)
- No gradient text, no glassmorphism overuse, no side-stripe borders
- Card usage is justified (purchase flow isolation, prize pool organization)
- Color strategy is restrained (amber accent for lottery excitement, semantic states only)

**Detector Scan Results:**  
Scanned 4 files via CLI detector — **0 violations found**. Clean report indicates issues are conceptual/design-level (Nielsen heuristics, cognitive load) rather than code-level anti-patterns (eyebrows, numbered markers, etc.).

---

## Overall Impression

**Strengths:**
- **Comprehensive state management**: All async states covered (loading, pending, success, fail, disabled), no blind spots
- **Trust through transparency**: Pity progress visible, prize probabilities disclosed, payment status real-time — aligns with "trust through clarity" brand principle
- **Restrained aesthetics**: Avoids over-gamification (no confetti explosions, no slot machine animations), maintains professional tone suitable for technical users
- **Information architecture**: Logical flow from overview (hero) → action (purchase card) → history (recent results)

**What's Working:**
- Payment method selector with visual distinction (icons, names, colors)
- Quantity selection UX (chips + custom input hybrid)
- First-purchase guarantee callout (amber Zap icon, progress bar)
- LocalStorage-based prop expiry system (technical elegance)

---

## Priority Issues

### P0 — Payment Dialog Lock-In (User Control Violation)

**Heuristic:** User control and freedom (score 2/4)  
**Problem:** During payment polling, dialog sets `showCloseButton={props.state.stage !== 'pending'}` and button shows "支付中" disabled state. User is trapped until payment resolves or times out.

**Impact:** If payment gateway hangs, user has no escape except force-closing browser tab. Violates Nielsen #3 (exits and undo).

**Fix:**
- Allow closing dialog during pending, but show confirmation: "支付仍在处理中，关闭后可在历史记录中查看结果。确定关闭吗？"
- Provide "后台继续" option that dismisses UI but keeps polling active
- Add timeout mechanism (e.g., 60s) that auto-releases lock with retry prompt

---

### P1 — Hero Section Cognitive Overload

**Heuristic:** Aesthetic and minimalist design (score 2/4)  
**Problem:** Hero grid (lines 99-196 in blind-box-page.tsx) presents:
- 4 metric cards (可用余额, Claude余额, 待抽取, 保底进度)
- 3 navigation buttons (查看钱包, 查看套餐, 查看奖池)
- 1 conditional panel (首抽保底 or 保底机制)
- Total: **8 interactive elements in ~200px height**

**Impact:** First-time users don't know where to focus. Cognitive load exceeds 7±2 limit. Metrics compete with primary CTA (立即购买 below fold).

**Fix:**
- **Simplify metrics**: Show only 2 metrics in hero (可用余额 + 待抽取 with highlight). Move Claude余额 and 保底进度 into collapsible "详细信息" section
- **Reduce buttons**: Keep only "查看奖池" in hero. Move 查看钱包/查看套餐 to breadcrumb navigation or sidebar
- **Elevate CTA**: Bring purchase card above fold by reducing hero height

---

### P2 — Prize Reveal Lacks Climax

**Heuristic:** Aesthetic and minimalist design (score 2/4), Help and documentation (score 3/4)  
**Problem:** Prize dialog (BlindBoxPrizeDialog, lines 240-337 in blind-box-dialogs.tsx) shows rewards as flat list with no visual hierarchy or animation. High-value rewards (subscription, pity) look identical to low-value props.

**Impact:** Lottery emotional journey fails to deliver payoff. Users don't feel excitement proportional to reward value.

**Fix:**
- **Stagger reveal**: Animate each reward card in sequence (100ms delay between items) with scale + fade entrance
- **Differentiate tiers**: Use gradient borders for rare rewards (subscription = gold border, pity = amber glow)
- **Add summary hero**: Show celebratory header when high-value reward hit ("恭喜获得月卡！" with larger font + icon)
- **Confetti sparingly**: Single burst on first high-value reward (not every item)

---

### P3 — Error Recovery Inadequate

**Heuristic:** Help users with errors (score 2/4)  
**Problem:** Payment failure shows generic "支付未完成，请重新发起购买" toast (line 75 in blind-box-page.tsx) with no context. User must navigate back, re-select quantity/method, retry from scratch.

**Impact:** Friction amplifies frustration. No guidance on why payment failed (network? insufficient balance? gateway issue?).

**Fix:**
- **Detailed error messages**: Parse backend error codes, show actionable text ("支付超时，请检查网络连接后重试" vs "余额不足，请充值后重试")
- **Retry in-place**: Add "重新支付" button in failure dialog that preserves quantity + method selections
- **Support link**: Provide "联系客服" quick action in error state

---

## Persona Red Flags

**Jordan (Technical User — API Quota Manager):**
- ✓ Finds pity progress and quota balance useful for forecasting costs
- ⚠️ Confused by "盲盒" framing when need is deterministic quota purchase — sees it as friction, not fun
- ❌ Frustrated by payment lock-in (P0) when multitasking across tabs

**Casey (General Consumer — Casual Lottery Participant):**
- ✓ Understands blind box metaphor immediately (cultural familiarity)
- ⚠️ Overwhelmed by hero metrics (P1) — doesn't care about Claude-specific balance, only wants to "抽一次看看"
- ❌ Prize reveal anticlimax (P2) makes rewards feel mundane, reduces repeat engagement

**Sam (First-Time User):**
- ❌ No onboarding: "保底" term unexplained, pity mechanics assumed knowledge
- ❌ Hero overload (P1) creates decision paralysis — closes page before scrolling to purchase card
- ⚠️ Error recovery gap (P3) causes abandonment when payment fails

---

## Minor Observations

**Visual Polish:**
- Prize pool dialog (BlindBoxPrizePoolDialog, lines 340-392) uses good information density, but probability badges are low-contrast (border-border/70 bg-background/60 text-muted-foreground) — bump to semantic colors (e.g., amber tint for rare items)
- Quantity chips (QuantityChip, lines 232-253 in blind-box-view.tsx) have good active/inactive states, but lack focus indicators for keyboard navigation

**Copy Clarity:**
- "应付金额" (lines 103-108 in blind-box-view.tsx) is formal financial language — consider "支付金额" for general audience
- "立即抽取 X 次" (line 211) could be "开启 X 个盲盒" for metaphor consistency

**Accessibility:**
- Payment dialog QR code (lines 191-214 in blind-box-dialogs.tsx) has no alt text fallback for screen readers
- Progress bars (lines 169, 188 in blind-box-page.tsx) lack aria-label describing what's being progressed

---

## Detector Analysis

CLI detector scanned:
- `src/features/wallet/blind-box-page.tsx` (240 lines)
- `src/features/wallet/components/blind-box-view.tsx` (254 lines)
- `src/features/wallet/components/blind-box-dialogs.tsx` (411 lines)
- `src/features/wallet/components/blind-box-card.tsx` (not modified, read for context)

**Findings:** 0 violations

**Interpretation:**  
No code-level anti-patterns detected (no eyebrows, no gradient text, no side-stripe borders, no numbered section markers). Issues identified are conceptual design problems (Nielsen heuristics, cognitive load) requiring UX restructuring, not find-and-replace refactors.

---

## Questions for Refinement

1. **Priority direction**: Do you want to fix P0 (payment lock-in) immediately as a blocker, or address P1 (hero overload) first for broader impact?

2. **User segment focus**: Should we optimize for Jordan (technical, efficiency-first) or Casey (casual, fun-first)? Current design tries to serve both but may be diluting each.

3. **Animation budget**: P2 suggests staggered prize reveal. Are you open to adding motion libraries (e.g., Framer Motion for orchestration), or must we stay with CSS transitions only?

4. **Scope**: Critique covers full flow (page + dialogs). Do you want polish pass on entire blind box feature, or isolate one surface (e.g., just the payment dialog)?

---

## Recommended Next Steps

**If priority is safety (P0):**  
Run `/impeccable polish blind-box-dialogs.tsx` focusing on payment dialog lock-in fix.

**If priority is conversion (P1 + P2):**  
Run `/impeccable polish blind-box-page.tsx` to simplify hero + enhance prize reveal.

**If priority is comprehensive:**  
Run `/impeccable polish src/features/wallet` to address all P0-P3 issues across the flow.

**If you want visual iteration first:**  
Start dev server (`bun run dev`) and use `/impeccable live` for in-browser element picking and variant generation.
