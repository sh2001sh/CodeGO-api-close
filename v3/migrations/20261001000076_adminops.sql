-- Owner-scoped favorites retain the original metadata model IDs.
CREATE SCHEMA v3_adminops;
CREATE TABLE v3_adminops.model_favorites (
    user_id bigint NOT NULL REFERENCES v3_identity.users(id) ON DELETE CASCADE,
    model_id bigint NOT NULL REFERENCES v3_catalog.models(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, model_id)
);
