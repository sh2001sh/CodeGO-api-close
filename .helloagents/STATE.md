# Main Goal
Improve the new-api overview so “今天的主要状态” reports trustworthy model-level health consistent with the Group Status page, and increase visual separation in the default dark theme.

# Current Status
- The overview health panel now consumes `/api/user/self/group-status`, uses the backend model status semantics, and prioritizes active models by request volume.
- Rows show model, group, recent success rate, request count, and the same status labels/colors as Group Status.
- The panel links to `/group-status`; its summary counts healthy active models instead of showing unrelated uptime data.
- Default dark theme surface, sidebar, popover, muted, border, input, and skeleton tokens have stronger separation while preserving the existing warm brand palette.
- Unit coverage was added for active-model filtering, sorting, status preservation, and the no-sample fallback.
- Focused tests, TypeScript typecheck, ESLint, Prettier, and the production build all pass.
- A dark-mode browser render was inspected at `output/playwright/overview-dark-model-status.png`; card and nested-surface hierarchy is visually distinct.

# Next Actions
- Review the changes and commit them when ready.

# Blockers
- None.
