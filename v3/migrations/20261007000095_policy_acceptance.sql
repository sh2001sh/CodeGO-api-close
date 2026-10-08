-- Only explicit user actions create acceptance records. Existing users are not backfilled.
CREATE TABLE v3_identity.policy_acceptances (
    user_id bigint NOT NULL REFERENCES v3_identity.users(id),
    document text NOT NULL CHECK (document IN ('terms', 'privacy', 'supplier')),
    version text NOT NULL CHECK (length(version) BETWEEN 1 AND 32),
    locale text NOT NULL CHECK (locale IN ('zh-HK', 'zh-CN', 'en', 'ja', 'ru', 'ko', 'fr', 'de', 'ar')),
    accepted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, document, version)
);
