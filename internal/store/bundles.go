package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/phrony-platform/runtime/internal/agentref"
)

const upsertBundle = `
INSERT INTO bundles (id, namespace, name, owner, labels)
VALUES ($1, $2, $3, $4, $5::jsonb)
ON CONFLICT (namespace, name) DO UPDATE SET
	owner = EXCLUDED.owner,
	labels = EXCLUDED.labels,
	updated_at = NOW()
RETURNING id
`

type UpsertBundleParams struct {
	ID        string          `json:"id"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Owner     string          `json:"owner"`
	Labels    json.RawMessage `json:"labels"`
}

func (q *Queries) UpsertBundle(ctx context.Context, arg UpsertBundleParams) (string, error) {
	row := q.db.QueryRowContext(ctx, upsertBundle,
		arg.ID,
		arg.Namespace,
		arg.Name,
		arg.Owner,
		arg.Labels,
	)
	var id string
	err := row.Scan(&id)
	return id, err
}

const bundleByNamespaceName = `
SELECT id, namespace, name
FROM bundles
WHERE namespace = $1 AND name = $2
`

type BundleByIDRow struct {
	ID        string
	Namespace string
	Name      string
}

type BundleListRow struct {
	ID        string
	Namespace string
	Name      string
	Owner     string
	CreatedAt time.Time
}

const listBundles = `
SELECT id, namespace, name, owner, created_at
FROM bundles
WHERE ($1::text = '' OR namespace = $1)
ORDER BY namespace, name
`

func (q *Queries) ListBundles(ctx context.Context, namespace string) ([]BundleListRow, error) {
	rows, err := q.db.QueryContext(ctx, listBundles, namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BundleListRow
	for rows.Next() {
		var row BundleListRow
		if err := rows.Scan(&row.ID, &row.Namespace, &row.Name, &row.Owner, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (q *Queries) BundleByNamespaceName(ctx context.Context, namespace, name string) (BundleByIDRow, error) {
	row := q.db.QueryRowContext(ctx, bundleByNamespaceName, namespace, name)
	var out BundleByIDRow
	err := row.Scan(&out.ID, &out.Namespace, &out.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return BundleByIDRow{}, err
	}
	return out, err
}

const insertBundleVersion = `
INSERT INTO bundle_versions (id, bundle_id, version, lock_hash, lock, root_member_version_id)
VALUES ($1, $2, $3, $4, $5::jsonb, NULLIF($6, '')::uuid)
RETURNING id
`

type InsertBundleVersionParams struct {
	ID                  string          `json:"id"`
	BundleID            string          `json:"bundle_id"`
	Version             string          `json:"version"`
	LockHash            string          `json:"lock_hash"`
	Lock                json.RawMessage `json:"lock"`
	RootMemberVersionID string          `json:"root_member_version_id"`
}

func (q *Queries) InsertBundleVersion(ctx context.Context, arg InsertBundleVersionParams) (string, error) {
	row := q.db.QueryRowContext(ctx, insertBundleVersion,
		arg.ID,
		arg.BundleID,
		arg.Version,
		arg.LockHash,
		arg.Lock,
		arg.RootMemberVersionID,
	)
	var id string
	err := row.Scan(&id)
	return id, err
}

const updateBundleVersionRootMember = `
UPDATE bundle_versions
SET root_member_version_id = $2
WHERE id = $1
`

func (q *Queries) UpdateBundleVersionRootMember(ctx context.Context, bundleVersionID, rootMemberVersionID string) error {
	_, err := q.db.ExecContext(ctx, updateBundleVersionRootMember, bundleVersionID, rootMemberVersionID)
	return err
}

const bundleVersionByLockHash = `
SELECT bv.id, bv.lock, bv.version
FROM bundle_versions bv
WHERE bv.bundle_id = $1 AND bv.lock_hash = $2
`

type BundleVersionByLockHashResult struct {
	ID      string
	Lock    json.RawMessage
	Version string
}

func (q *Queries) BundleVersionByLockHash(ctx context.Context, bundleID, lockHash string) (BundleVersionByLockHashResult, error) {
	row := q.db.QueryRowContext(ctx, bundleVersionByLockHash, bundleID, lockHash)
	var out BundleVersionByLockHashResult
	err := row.Scan(&out.ID, &out.Lock, &out.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return BundleVersionByLockHashResult{}, err
	}
	return out, err
}

const bundleVersionIDByLockHash = `
SELECT bv.id, bv.root_member_version_id, bv.lock_hash, bv.version
FROM bundle_versions bv
INNER JOIN bundles b ON b.id = bv.bundle_id
WHERE b.namespace = $1 AND b.name = $2 AND bv.lock_hash = $3
`

const bundleVersionIDBySemver = `
SELECT bv.id, bv.root_member_version_id, bv.lock_hash, bv.version
FROM bundle_versions bv
INNER JOIN bundles b ON b.id = bv.bundle_id
WHERE b.namespace = $1 AND b.name = $2 AND bv.version = $3
`

const bundleVersionBySemver = `
SELECT bv.id, bv.lock_hash
FROM bundle_versions bv
WHERE bv.bundle_id = $1 AND bv.version = $2
`

type BundleVersionBySemverResult struct {
	ID       string
	LockHash string
}

func (q *Queries) BundleVersionBySemver(ctx context.Context, bundleID, version string) (BundleVersionBySemverResult, error) {
	row := q.db.QueryRowContext(ctx, bundleVersionBySemver, bundleID, version)
	var out BundleVersionBySemverResult
	err := row.Scan(&out.ID, &out.LockHash)
	if errors.Is(err, sql.ErrNoRows) {
		return BundleVersionBySemverResult{}, err
	}
	return out, err
}

type BundleVersionLookupResult struct {
	ID                  string
	RootMemberVersionID string
	LockHash            string
	Version             string
}

func (q *Queries) BundleVersionIDByLabel(ctx context.Context, namespace, name, label string) (BundleVersionLookupResult, error) {
	if agentref.IsLockHashVersion(label) {
		return q.bundleVersionIDByLockHash(ctx, namespace, name, label)
	}
	return q.bundleVersionIDBySemver(ctx, namespace, name, label)
}

func (q *Queries) bundleVersionIDByLockHash(ctx context.Context, namespace, name, lockHash string) (BundleVersionLookupResult, error) {
	row := q.db.QueryRowContext(ctx, bundleVersionIDByLockHash, namespace, name, lockHash)
	return scanBundleVersionLookup(row)
}

func (q *Queries) bundleVersionIDBySemver(ctx context.Context, namespace, name, version string) (BundleVersionLookupResult, error) {
	row := q.db.QueryRowContext(ctx, bundleVersionIDBySemver, namespace, name, version)
	return scanBundleVersionLookup(row)
}

func scanBundleVersionLookup(row *sql.Row) (BundleVersionLookupResult, error) {
	var out BundleVersionLookupResult
	var root sql.NullString
	err := row.Scan(&out.ID, &root, &out.LockHash, &out.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return BundleVersionLookupResult{}, err
	}
	if err != nil {
		return BundleVersionLookupResult{}, err
	}
	if root.Valid {
		out.RootMemberVersionID = root.String
	}
	return out, nil
}

const insertVendoredAgentVersion = `
INSERT INTO agent_versions (id, agent_id, version, content_hash, manifest, origin, bundle_version_id)
VALUES ($1, NULL, $2, $3, $4::jsonb, 'vendored', $5)
RETURNING id
`

type InsertVendoredAgentVersionParams struct {
	ID              string          `json:"id"`
	Version         string          `json:"version"`
	ContentHash     string          `json:"content_hash"`
	Manifest        json.RawMessage `json:"manifest"`
	BundleVersionID string          `json:"bundle_version_id"`
}

func (q *Queries) InsertVendoredAgentVersion(ctx context.Context, arg InsertVendoredAgentVersionParams) (string, error) {
	row := q.db.QueryRowContext(ctx, insertVendoredAgentVersion,
		arg.ID,
		arg.Version,
		arg.ContentHash,
		arg.Manifest,
		arg.BundleVersionID,
	)
	var id string
	err := row.Scan(&id)
	return id, err
}

const insertBundleMember = `
INSERT INTO bundle_members (bundle_version_id, child_name, member_version_id, ref, origin, is_root)
VALUES ($1, $2, $3, $4, $5, $6)
`

type InsertBundleMemberParams struct {
	BundleVersionID string `json:"bundle_version_id"`
	ChildName       string `json:"child_name"`
	MemberVersionID string `json:"member_version_id"`
	Ref             string `json:"ref"`
	Origin          string `json:"origin"`
	IsRoot          bool   `json:"is_root"`
}

func (q *Queries) InsertBundleMember(ctx context.Context, arg InsertBundleMemberParams) error {
	_, err := q.db.ExecContext(ctx, insertBundleMember,
		arg.BundleVersionID,
		arg.ChildName,
		arg.MemberVersionID,
		arg.Ref,
		arg.Origin,
		arg.IsRoot,
	)
	return err
}

const insertBundleDeployment = `
INSERT INTO bundle_deployments (id, bundle_id, bundle_version_id, action, actor)
VALUES ($1, $2, $3, $4, $5)
RETURNING id
`

func (q *Queries) InsertBundleDeployment(ctx context.Context, id, bundleID, bundleVersionID, action, actor string) (string, error) {
	row := q.db.QueryRowContext(ctx, insertBundleDeployment, id, bundleID, bundleVersionID, action, actor)
	var deploymentID string
	err := row.Scan(&deploymentID)
	return deploymentID, err
}

const activeBundleVersion = `
SELECT bv.id, bv.version, bv.lock_hash, bv.root_member_version_id
FROM bundle_deployments bd
INNER JOIN bundle_versions bv ON bv.id = bd.bundle_version_id
INNER JOIN bundles b ON b.id = bd.bundle_id
WHERE b.namespace = $1 AND b.name = $2
ORDER BY bd.created_at DESC
LIMIT 1
`

type ActiveBundleVersionResult struct {
	BundleVersionID     string
	Version             string
	LockHash            string
	RootMemberVersionID string
}

func (q *Queries) ActiveBundleVersion(ctx context.Context, namespace, name string) (ActiveBundleVersionResult, error) {
	row := q.db.QueryRowContext(ctx, activeBundleVersion, namespace, name)
	var out ActiveBundleVersionResult
	var root sql.NullString
	err := row.Scan(&out.BundleVersionID, &out.Version, &out.LockHash, &root)
	if errors.Is(err, sql.ErrNoRows) {
		return ActiveBundleVersionResult{}, err
	}
	if err != nil {
		return ActiveBundleVersionResult{}, err
	}
	if root.Valid {
		out.RootMemberVersionID = root.String
	}
	return out, nil
}

const activeBundleDeploymentDetail = `
SELECT bv.version, bv.lock_hash, bd.created_at, bd.actor
FROM bundle_deployments bd
INNER JOIN bundle_versions bv ON bv.id = bd.bundle_version_id
INNER JOIN bundles b ON b.id = bd.bundle_id
WHERE b.namespace = $1 AND b.name = $2
ORDER BY bd.created_at DESC
LIMIT 1
`

type ActiveBundleDeploymentDetail struct {
	Version    string
	LockHash   string
	DeployedAt time.Time
	Actor      string
}

func (q *Queries) ActiveBundleDeploymentDetail(ctx context.Context, namespace, name string) (ActiveBundleDeploymentDetail, error) {
	row := q.db.QueryRowContext(ctx, activeBundleDeploymentDetail, namespace, name)
	var out ActiveBundleDeploymentDetail
	err := row.Scan(&out.Version, &out.LockHash, &out.DeployedAt, &out.Actor)
	if errors.Is(err, sql.ErrNoRows) {
		return ActiveBundleDeploymentDetail{}, err
	}
	return out, err
}

const previousActiveBundleVersion = `
WITH active AS (
	SELECT bundle_version_id
	FROM bundle_deployments
	WHERE bundle_id = $1
	ORDER BY created_at DESC
	LIMIT 1
)
SELECT bd.bundle_version_id
FROM bundle_deployments bd
WHERE bd.bundle_id = $1
	AND bd.bundle_version_id <> (SELECT bundle_version_id FROM active)
ORDER BY bd.created_at DESC
LIMIT 1
`

func (q *Queries) PreviousActiveBundleVersion(ctx context.Context, bundleID string) (string, error) {
	row := q.db.QueryRowContext(ctx, previousActiveBundleVersion, bundleID)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return id, err
}

type BundleVersionListRow struct {
	ID        string
	Version   string
	LockHash  string
	CreatedAt time.Time
}

const listBundleVersions = `
SELECT id, version, lock_hash, created_at
FROM bundle_versions
WHERE bundle_id = $1
ORDER BY created_at DESC
`

func (q *Queries) ListBundleVersions(ctx context.Context, bundleID string) ([]BundleVersionListRow, error) {
	rows, err := q.db.QueryContext(ctx, listBundleVersions, bundleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BundleVersionListRow
	for rows.Next() {
		var row BundleVersionListRow
		if err := rows.Scan(&row.ID, &row.Version, &row.LockHash, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

type BundleDeploymentListRow struct {
	Version   string
	LockHash  string
	Action    string
	Actor     string
	CreatedAt time.Time
}

const listBundleDeployments = `
SELECT bv.version, bv.lock_hash, bd.action, bd.actor, bd.created_at
FROM bundle_deployments bd
INNER JOIN bundle_versions bv ON bv.id = bd.bundle_version_id
WHERE bd.bundle_id = $1
ORDER BY bd.created_at DESC
`

func (q *Queries) ListBundleDeployments(ctx context.Context, bundleID string) ([]BundleDeploymentListRow, error) {
	rows, err := q.db.QueryContext(ctx, listBundleDeployments, bundleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BundleDeploymentListRow
	for rows.Next() {
		var row BundleDeploymentListRow
		if err := rows.Scan(&row.Version, &row.LockHash, &row.Action, &row.Actor, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

const listBundleReferencesForMemberVersion = `
SELECT b.namespace, b.name, bv.lock_hash
FROM bundle_members bm
INNER JOIN bundle_versions bv ON bv.id = bm.bundle_version_id
INNER JOIN bundles b ON b.id = bv.bundle_id
WHERE bm.member_version_id = $1
ORDER BY b.namespace, b.name, bv.lock_hash
`

type BundleMemberReference struct {
	Namespace string
	Name      string
	LockHash  string
}

// ListBundleReferencesForMemberVersion returns bundles that retain the given
// agent version via bundle_members (used to block retirement/GC).
const listBundleMemberManifests = `
SELECT av.manifest, bm.child_name, bm.origin
FROM bundle_members bm
JOIN agent_versions av ON av.id = bm.member_version_id
WHERE bm.bundle_version_id = $1
ORDER BY bm.child_name
`

type BundleMemberManifestRow struct {
	Manifest  json.RawMessage
	ChildName string
	Origin    string
}

func (q *Queries) ListBundleMemberManifests(ctx context.Context, bundleVersionID string) ([]BundleMemberManifestRow, error) {
	rows, err := q.db.QueryContext(ctx, listBundleMemberManifests, bundleVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BundleMemberManifestRow
	for rows.Next() {
		var row BundleMemberManifestRow
		if err := rows.Scan(&row.Manifest, &row.ChildName, &row.Origin); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (q *Queries) ListBundleReferencesForMemberVersion(ctx context.Context, memberVersionID string) ([]BundleMemberReference, error) {
	rows, err := q.db.QueryContext(ctx, listBundleReferencesForMemberVersion, memberVersionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BundleMemberReference
	for rows.Next() {
		var row BundleMemberReference
		if err := rows.Scan(&row.Namespace, &row.Name, &row.LockHash); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
