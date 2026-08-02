package storage

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"codeflux.dev/codeflux/internal/domain"
)

// InstructionApprovalScope bounds how long one first-use approval lasts.
type InstructionApprovalScope string

const (
	// InstructionApprovalSingleUse expires on first consumption.
	InstructionApprovalSingleUse InstructionApprovalScope = "single-use"
	// InstructionApprovalRevision lasts while the repository stays at the
	// approved revision with the approved bytes.
	InstructionApprovalRevision InstructionApprovalScope = "revision"
)

// gitRevision matches the forty-character revisions this schema stores.
var gitRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)

// contentDigest matches a lowercase SHA-256 hex digest.
var contentDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// GrantRepositoryInstructionApproval is one recorded first-use decision.
type GrantRepositoryInstructionApproval struct {
	ID                 string
	ProjectID          domain.ProjectID
	RepositoryID       domain.RepositoryID
	RepositoryRevision string
	InstructionPath    string
	ContentSHA256      string
	Scope              InstructionApprovalScope
	ReasonRedacted     string
}

// RepositoryInstructionApproval is a durable approval as stored.
type RepositoryInstructionApproval struct {
	ID                 string
	ProjectID          domain.ProjectID
	RepositoryID       domain.RepositoryID
	RepositoryRevision string
	InstructionPath    string
	ContentSHA256      string
	Scope              InstructionApprovalScope
	Consumptions       int
	Revoked            bool
}

// Usable reports whether the approval may still admit its instruction.
//
// A revoked approval never can. A single-use approval that has already been
// consumed never can again, which is what makes replaying one impossible
// rather than merely discouraged.
func (approval RepositoryInstructionApproval) Usable() bool {
	if approval.Revoked {
		return false
	}
	if approval.Scope == InstructionApprovalSingleUse && approval.Consumptions > 0 {
		return false
	}
	return true
}

// GrantRepositoryInstructionApproval records one first-use approval (M08-049,
// AUDIT-011).
//
// The grant is immutable. Repository content is untrusted, so an approval that
// could be edited afterwards would be worth no more than the caller-supplied
// path list it replaces.
func (repositories *Repositories) GrantRepositoryInstructionApproval(
	ctx context.Context,
	input GrantRepositoryInstructionApproval,
) (RepositoryInstructionApproval, error) {
	if err := input.validate(); err != nil {
		return RepositoryInstructionApproval{}, err
	}
	_, micros := repositories.timestamp()

	var reason any
	if strings.TrimSpace(input.ReasonRedacted) != "" {
		reason = input.ReasonRedacted
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO repository_instruction_approvals
		   (id, project_id, repository_id, repository_revision, instruction_path,
		    content_sha256, scope, granted_at_unix_micros, reason_redacted)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ID, input.ProjectID.String(), input.RepositoryID.String(),
		input.RepositoryRevision, normalizeInstructionPath(input.InstructionPath),
		input.ContentSHA256, string(input.Scope), micros, reason,
	); err != nil {
		return RepositoryInstructionApproval{}, classify("grant repository instruction approval", err)
	}
	return RepositoryInstructionApproval{
		ID:                 input.ID,
		ProjectID:          input.ProjectID,
		RepositoryID:       input.RepositoryID,
		RepositoryRevision: input.RepositoryRevision,
		InstructionPath:    normalizeInstructionPath(input.InstructionPath),
		ContentSHA256:      input.ContentSHA256,
		Scope:              input.Scope,
	}, nil
}

func (input GrantRepositoryInstructionApproval) validate() error {
	switch {
	case strings.TrimSpace(input.ID) == "":
		return errors.New("approval ID must not be empty")
	case input.ProjectID.IsZero():
		return errors.New("project ID must not be empty")
	case input.RepositoryID.IsZero():
		return errors.New("repository ID must not be empty")
	case !gitRevision.MatchString(input.RepositoryRevision):
		return errors.New("repository revision must be a forty-character Git revision")
	case !contentDigest.MatchString(input.ContentSHA256):
		return errors.New("content digest must be a SHA-256 hex digest")
	}
	path := normalizeInstructionPath(input.InstructionPath)
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
		return errors.New("instruction path must be repository-relative and must not escape it")
	}
	switch input.Scope {
	case InstructionApprovalSingleUse, InstructionApprovalRevision:
	default:
		return errors.New("approval scope must be single-use or revision")
	}
	// The reason is optional: the schema stores it nullable, and requiring
	// prose would push a caller into writing filler rather than recording a
	// real justification.
	if strings.TrimSpace(input.ReasonRedacted) == "" {
		return nil
	}
	return validateBounded("approval reason", input.ReasonRedacted, 2048)
}

// FindRepositoryInstructionApproval resolves the approval that would admit one
// instruction, or reports that none does.
//
// Every binding is part of the lookup. An approval granted for a different
// project, a different repository, a different revision, or different bytes is
// not this approval, and returning it would reintroduce the hole this replaces.
func (repositories *Repositories) FindRepositoryInstructionApproval(
	ctx context.Context,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	repositoryRevision string,
	instructionPath string,
	contentSHA256 string,
) (RepositoryInstructionApproval, bool, error) {
	if projectID.IsZero() || repositoryID.IsZero() {
		return RepositoryInstructionApproval{}, false,
			errors.New("project and repository are required to resolve an approval")
	}
	if !gitRevision.MatchString(repositoryRevision) || !contentDigest.MatchString(contentSHA256) {
		return RepositoryInstructionApproval{}, false, nil
	}

	var (
		approval RepositoryInstructionApproval
		scope    string
		revoked  sql.NullInt64
	)
	err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT a.id, a.scope, a.revoked_at_unix_micros,
		        (SELECT COUNT(*) FROM repository_instruction_approval_consumptions c
		          WHERE c.approval_id = a.id)
		   FROM repository_instruction_approvals a
		  WHERE a.project_id = ? AND a.repository_id = ?
		    AND a.repository_revision = ? AND a.instruction_path = ?
		    AND a.content_sha256 = ?`,
		projectID.String(), repositoryID.String(), repositoryRevision,
		normalizeInstructionPath(instructionPath), contentSHA256,
	).Scan(&approval.ID, &scope, &revoked, &approval.Consumptions)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RepositoryInstructionApproval{}, false, nil
		}
		return RepositoryInstructionApproval{}, false,
			classify("resolve repository instruction approval", err)
	}

	approval.ProjectID = projectID
	approval.RepositoryID = repositoryID
	approval.RepositoryRevision = repositoryRevision
	approval.InstructionPath = normalizeInstructionPath(instructionPath)
	approval.ContentSHA256 = contentSHA256
	approval.Scope = InstructionApprovalScope(scope)
	approval.Revoked = revoked.Valid
	return approval, true, nil
}

// ConsumeRepositoryInstructionApproval records that an approval admitted its
// instruction into one context manifest.
//
// It refuses an approval that is no longer usable rather than recording the
// consumption and letting the caller decide, because the caller deciding is
// the failure mode AUDIT-011 describes.
func (repositories *Repositories) ConsumeRepositoryInstructionApproval(
	ctx context.Context,
	consumptionID string,
	approval RepositoryInstructionApproval,
	consumedBy string,
) error {
	if strings.TrimSpace(consumptionID) == "" || strings.TrimSpace(approval.ID) == "" {
		return errors.New("consumption and approval identities are required")
	}
	if strings.TrimSpace(consumedBy) == "" {
		return errors.New("a consumer identity is required")
	}
	if !approval.Usable() {
		return typedError(ErrConstraint, "consume repository instruction approval",
			errors.New("approval is revoked or already spent"))
	}
	_, micros := repositories.timestamp()
	if _, err := repositories.database.sql.ExecContext(
		ctx,
		`INSERT INTO repository_instruction_approval_consumptions
		   (id, approval_id, consumed_by, consumed_at_unix_micros)
		 VALUES (?, ?, ?, ?)`,
		consumptionID, approval.ID, consumedBy, micros,
	); err != nil {
		return classify("consume repository instruction approval", err)
	}
	return nil
}

// RevokeRepositoryInstructionApproval withdraws an approval without deleting
// the record of it having existed.
func (repositories *Repositories) RevokeRepositoryInstructionApproval(
	ctx context.Context,
	approvalID string,
) error {
	if strings.TrimSpace(approvalID) == "" {
		return errors.New("approval ID must not be empty")
	}
	_, micros := repositories.timestamp()
	result, err := repositories.database.sql.ExecContext(
		ctx,
		`UPDATE repository_instruction_approvals
		    SET revoked_at_unix_micros = ?
		  WHERE id = ? AND revoked_at_unix_micros IS NULL`,
		micros, approvalID,
	)
	if err != nil {
		return classify("revoke repository instruction approval", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return classify("confirm approval revocation", err)
	}
	if affected == 0 {
		return typedError(ErrNotFound, "revoke repository instruction approval",
			errors.New("no live approval with that identity"))
	}
	return nil
}

// normalizeInstructionPath renders a repository-relative path the one way the
// table stores it, so a lookup cannot miss an approval over a separator.
func normalizeInstructionPath(path string) string {
	return strings.TrimSpace(strings.ReplaceAll(path, `\`, "/"))
}

// InstructionApprovalResolverFor binds one project, repository, and consumer
// to the durable approval table, producing the resolver
// workspace.ContextQuery expects.
//
// The scope is fixed at construction rather than passed per call, because the
// project and repository are the two bindings a caller must not be able to
// vary while selecting context: an approval granted in one repository must
// never admit an instruction in another.
func (repositories *Repositories) InstructionApprovalResolverFor(
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
	consumedBy string,
) *InstructionApprovalResolver {
	return &InstructionApprovalResolver{
		repositories: repositories,
		projectID:    projectID,
		repositoryID: repositoryID,
		consumedBy:   consumedBy,
	}
}

// InstructionApprovalResolver answers first-use approval from SQLite.
type InstructionApprovalResolver struct {
	repositories *Repositories
	projectID    domain.ProjectID
	repositoryID domain.RepositoryID
	consumedBy   string
	// newID supplies consumption identities. It is a field so a test can make
	// them deterministic without reaching into the database.
	newID func() (string, error)
}

// InstructionApproved reports whether the instruction may be admitted, and
// records the consumption when it may.
//
// Consumption is recorded as part of answering rather than left to the caller.
// A caller that could decide whether to record it could replay a single-use
// approval by declining to, which is the third case AUDIT-011 names.
func (resolver *InstructionApprovalResolver) InstructionApproved(
	ctx context.Context,
	repositoryRevision string,
	instructionPath string,
	contentSHA256 string,
) (bool, error) {
	if resolver == nil || resolver.repositories == nil {
		return false, nil
	}
	approval, found, err := resolver.repositories.FindRepositoryInstructionApproval(
		ctx, resolver.projectID, resolver.repositoryID,
		repositoryRevision, instructionPath, contentSHA256,
	)
	if err != nil {
		return false, err
	}
	if !found || !approval.Usable() {
		return false, nil
	}

	consumptionID, err := resolver.nextID()
	if err != nil {
		return false, err
	}
	if err := resolver.repositories.ConsumeRepositoryInstructionApproval(
		ctx, consumptionID, approval, resolver.consumedBy,
	); err != nil {
		return false, err
	}
	return true, nil
}

func (resolver *InstructionApprovalResolver) nextID() (string, error) {
	if resolver.newID != nil {
		return resolver.newID()
	}
	id, err := domain.NewEventID()
	if err != nil {
		return "", err
	}
	return "ric-" + id.String(), nil
}
