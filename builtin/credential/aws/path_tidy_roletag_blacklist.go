package awsauth

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/hashicorp/errwrap"
	"github.com/hashicorp/vault/logical"
	"github.com/hashicorp/vault/logical/framework"
)

func pathTidyRoletagBlacklist(b *backend) *framework.Path {
	return &framework.Path{
		Pattern: "tidy/roletag-blacklist$",
		Fields: map[string]*framework.FieldSchema{
			"safety_buffer": &framework.FieldSchema{
				Type:    framework.TypeDurationSecond,
				Default: 259200, // 72h
				Description: `The amount of extra time that must have passed beyond the roletag
expiration, before it is removed from the backend storage.`,
			},
		},

		Callbacks: map[logical.Operation]framework.OperationFunc{
			logical.UpdateOperation: b.pathTidyRoletagBlacklistUpdate,
		},

		HelpSynopsis:    pathTidyRoletagBlacklistSyn,
		HelpDescription: pathTidyRoletagBlacklistDesc,
	}
}

// tidyBlacklistRoleTag is used to clean-up the entries in the role tag blacklist.
func (b *backend) tidyBlacklistRoleTag(ctx context.Context, s logical.Storage, safety_buffer int) error {
	grabbed := atomic.CompareAndSwapUint32(&b.tidyBlacklistCASGuard, 0, 1)
	if grabbed {
		defer atomic.StoreUint32(&b.tidyBlacklistCASGuard, 0)
	} else {
		return fmt.Errorf("roletag blacklist tidy operation already running")
	}

	bufferDuration := time.Duration(safety_buffer) * time.Second
	tags, err := s.List(ctx, "blacklist/roletag/")
	if err != nil {
		return err
	}

	for _, tag := range tags {
		tagEntry, err := s.Get(ctx, "blacklist/roletag/"+tag)
		if err != nil {
			return errwrap.Wrapf(fmt.Sprintf("error fetching tag %q: {{err}}", tag), err)
		}

		if tagEntry == nil {
			return fmt.Errorf("tag entry for tag %q is nil", tag)
		}

		if tagEntry.Value == nil || len(tagEntry.Value) == 0 {
			return fmt.Errorf("found entry for tag %q but actual tag is empty", tag)
		}

		var result roleTagBlacklistEntry
		if err := tagEntry.DecodeJSON(&result); err != nil {
			return err
		}

		if time.Now().After(result.ExpirationTime.Add(bufferDuration)) {
			if err := s.Delete(ctx, "blacklist/roletag"+tag); err != nil {
				return errwrap.Wrapf(fmt.Sprintf("error deleting tag %q from storage: {{err}}", tag), err)
			}
		}
	}

	return nil
}

// pathTidyRoletagBlacklistUpdate is used to clean-up the entries in the role tag blacklist.
func (b *backend) pathTidyRoletagBlacklistUpdate(ctx context.Context, req *logical.Request, data *framework.FieldData) (*logical.Response, error) {
	return nil, b.tidyBlacklistRoleTag(ctx, req.Storage, data.Get("safety_buffer").(int))
}

const pathTidyRoletagBlacklistSyn = `
Clean-up the blacklist role tag entries.
`

const pathTidyRoletagBlacklistDesc = `
When a role tag is blacklisted, the expiration time of the blacklist entry is
set based on the maximum 'max_ttl' value set on: the role, the role tag and the
backend's mount.

When this endpoint is invoked, all the entries that are expired will be deleted.
A 'safety_buffer' (duration in seconds) can be provided, to ensure deletion of
only those entries that are expired before 'safety_buffer' seconds. 
`
