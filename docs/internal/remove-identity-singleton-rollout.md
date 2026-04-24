# Remove `identity:__identity__` Rollout

This change deletes the shared startup credential subject from runtime use.

New behavior:
- workflow execution refs keep their actual system subject as both `subject_id` and `credential_subject_id`
- canonical `external_credentials` rows are keyed by `subject_id`, not `identity_id`

Examples:

```go
// Old startup runtime shape.
&principal.Principal{
    SubjectID:           "system:config",
    CredentialSubjectID: "identity:__identity__",
}

// New startup runtime shape.
&principal.Principal{
    SubjectID:           "system:config",
    CredentialSubjectID: "system:config",
}
```

```sql
-- Old canonical credential lookup key.
(identity_id, plugin, connection, instance)

-- New canonical credential lookup key.
(subject_id, plugin, connection, instance)
```

## Rollout shape

This is a cutover-style deploy for `external_credentials`.

Why:
- old code reads and writes `external_credentials.identity_id`
- new code reads and writes `external_credentials.subject_id`
- mixed old/new overlap after the schema flip is not safe

## Required precheck

Abort the rollout if any legacy startup token rows still exist:

```sql
SELECT COUNT(*) AS legacy_startup_tokens
FROM integration_tokens
WHERE subject_id = 'identity:__identity__';
```

This branch does not rewrite those rows in code. The rollout is only safe if that count is `0`.

If the count is not `0`, do not deploy this branch as-is. Those rows will become unreachable after startup/system workflow credentials switch to `subject_id='system:config'`.

Required manual rewrite before deploy if the count is nonzero:

```sql
UPDATE integration_tokens
SET subject_id = 'system:config'
WHERE subject_id = 'identity:__identity__';
```

After that rewrite, rerun the precheck and make sure it returns `0`.

## Deploy steps

1. Take a snapshot / backup of the `external_credentials` table.
2. Drain old app revisions so old code is no longer serving against the store.
   No rolling old/new overlap after the schema flip.
3. Run the manual backfill from `integration_tokens` into `external_credentials`.
4. Deploy the new revision.

## Exact cutover SQL

Run this only after old revisions are drained and immediately before the new
revision is deployed.

```sql
RENAME TABLE external_credentials TO external_credentials_backup_20260422_1237;

CREATE TABLE external_credentials (
  id varchar(255) NOT NULL,
  subject_id varchar(255) NOT NULL,
  plugin varchar(255) NOT NULL,
  connection varchar(255) NOT NULL,
  instance varchar(255) NOT NULL,
  auth_type longtext NOT NULL,
  payload_encrypted longtext,
  scopes longtext,
  expires_at datetime(6) DEFAULT NULL,
  last_refreshed_at datetime(6) DEFAULT NULL,
  refresh_error_count bigint DEFAULT NULL,
  metadata_json longtext,
  created_at datetime(6) DEFAULT NULL,
  updated_at datetime(6) DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY idx_external_credentials_by_lookup (subject_id(128), plugin(128), connection(128), instance(128)),
  KEY idx_external_credentials_by_subject (subject_id),
  KEY idx_external_credentials_by_subject_plugin (subject_id(128), plugin(128)),
  KEY idx_external_credentials_by_subject_connection (subject_id(128), plugin(128), connection(128))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
```

The backup table is expected to keep the pre-cutover rows and indexes exactly as
they were. The new `external_credentials` table must be populated manually
before the new revision serves traffic; this rollout no longer relies on
startup rebuilding canonical rows from `integration_tokens`.

## Required verification

Check that canonical rows match the deduped readable source rows:

```sql
WITH ranked_tokens AS (
  SELECT
    subject_id,
    integration,
    connection,
    instance,
    access_token_encrypted,
    refresh_token_encrypted,
    ROW_NUMBER() OVER (
      PARTITION BY subject_id, integration, connection, instance
      ORDER BY updated_at DESC, created_at DESC, id ASC
    ) AS rn
  FROM integration_tokens
),
deduped_tokens AS (
  SELECT *
  FROM ranked_tokens
  WHERE rn = 1
),
readable_tokens AS (
  SELECT COUNT(*) AS n
  FROM deduped_tokens
  WHERE access_token_encrypted IS NOT NULL
)
SELECT
  (SELECT n FROM readable_tokens) AS readable_tokens,
  (SELECT COUNT(*) FROM external_credentials) AS canonical_external_credentials;
```

Those counts should match.

Also verify that startup-owned credentials are now subject-owned:

```sql
SELECT subject_id, plugin, connection, instance
FROM external_credentials
WHERE subject_id LIKE 'system:%'
ORDER BY subject_id, plugin, connection, instance;
```

## Rollback

If verification fails:
- restore the `external_credentials` backup
- redeploy the previous revision
- do not leave the new schema live while old and new revisions overlap
