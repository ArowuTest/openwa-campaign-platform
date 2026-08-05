package campaignworkspace

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"campaign-platform/internal/campaign"
	"campaign-platform/internal/shared/id"
)

type NoteCategory string

const (
	NoteGeneral    NoteCategory = "GENERAL"
	NoteCompliance NoteCategory = "COMPLIANCE"
	NoteFinance    NoteCategory = "FINANCE"
	NoteTechnical  NoteCategory = "TECHNICAL"
)

type Note struct {
	ID         string       `json:"id"`
	CampaignID string       `json:"campaignId"`
	Category   NoteCategory `json:"category"`
	Body       string       `json:"body"`
	CreatedBy  string       `json:"createdBy"`
	CreatedAt  time.Time    `json:"createdAt"`
}

type ArchiveRecord struct {
	CampaignID string     `json:"campaignId"`
	Archived   bool       `json:"archived"`
	ArchivedBy string     `json:"archivedBy,omitempty"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Version    int64      `json:"version"`
}

type Workspace struct {
	CampaignID string        `json:"campaignId"`
	Tags       []string      `json:"tags"`
	Archive    ArchiveRecord `json:"archive"`
	Notes      []Note        `json:"notes,omitempty"`
}

type AddNoteInput struct {
	Category NoteCategory `json:"category"`
	Body     string       `json:"body"`
	ActorID  string       `json:"-"`
}

type SetTagsInput struct {
	Tags            []string `json:"tags"`
	ActorID         string   `json:"-"`
	Reason          string   `json:"reason"`
	ExpectedVersion int64    `json:"expectedVersion"`
}

type ArchiveInput struct {
	ActorID         string `json:"-"`
	Reason          string `json:"reason"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,39}$`)

func NormalizeTags(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, errors.New("a campaign may have at most 20 tags")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		if !tagPattern.MatchString(v) {
			return nil, errors.New("tags must be 2-40 characters using lowercase letters, numbers, dot, underscore or hyphen")
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out, nil
}

func NewNote(campaignID string, in AddNoteInput, now time.Time) (Note, error) {
	if strings.TrimSpace(campaignID) == "" || strings.TrimSpace(in.ActorID) == "" {
		return Note{}, errors.New("campaign and actor are required")
	}
	body := strings.TrimSpace(in.Body)
	if len(body) < 3 || len(body) > 4000 {
		return Note{}, errors.New("note body must be between 3 and 4000 characters")
	}
	switch in.Category {
	case NoteGeneral, NoteCompliance, NoteFinance, NoteTechnical:
	default:
		return Note{}, errors.New("unsupported note category")
	}
	identifier, err := id.New()
	if err != nil {
		return Note{}, err
	}
	return Note{ID: identifier, CampaignID: campaignID, Category: in.Category, Body: body, CreatedBy: strings.TrimSpace(in.ActorID), CreatedAt: now.UTC()}, nil
}

func CanArchive(status campaign.Status) bool {
	return status == campaign.StatusCompleted || status == campaign.StatusCompletedWithExceptions || status == campaign.StatusCancelled
}
