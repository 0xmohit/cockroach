// Copyright 2016 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package tree

import (
	"github.com/cockroachdb/errors"
	"github.com/google/go-cmp/cmp"
)

// DescriptorCoverage specifies whether or not a subset of descriptors were
// requested or if all the descriptors were requested, so all the descriptors
// are covered in a given backup.
type DescriptorCoverage int32

const (
	// RequestedDescriptors table coverage means that the backup is not
	// guaranteed to have all of the cluster data. This can be accomplished by
	// backing up a specific subset of tables/databases. Note that even if all
	// of the tables and databases have been included in the backup manually, a
	// backup is not said to have complete table coverage unless it was created
	// by a `BACKUP TO` command.
	RequestedDescriptors DescriptorCoverage = iota
	// AllDescriptors table coverage means that backup is guaranteed to have all the
	// relevant data in the cluster. These can only be created by running a
	// full cluster backup with `BACKUP TO`.
	AllDescriptors
)

// BackupOptions describes options for the BACKUP execution.
type BackupOptions struct {
	CaptureRevisionHistory bool
	EncryptionPassphrase   Expr
	Detached               bool
	EncryptionKMSURI       StringOrPlaceholderOptList
}

var _ NodeFormatter = &BackupOptions{}

// Backup represents a BACKUP statement.
type Backup struct {
	Targets            *TargetList
	DescriptorCoverage DescriptorCoverage
	To                 StringOrPlaceholderOptList
	IncrementalFrom    Exprs
	AsOf               AsOfClause
	Options            BackupOptions
	Nested             bool
	AppendToLatest     bool
}

var _ Statement = &Backup{}

// Format implements the NodeFormatter interface.
func (node *Backup) Format(ctx *FmtCtx) {
	ctx.WriteString("BACKUP ")
	if node.Targets != nil {
		ctx.FormatNode(node.Targets)
		ctx.WriteString(" ")
	}
	if node.Nested {
		ctx.WriteString("INTO ")
		if node.AppendToLatest {
			ctx.WriteString("LATEST IN ")
		}
	} else {
		ctx.WriteString("TO ")
	}
	ctx.FormatNode(&node.To)
	if node.AsOf.Expr != nil {
		ctx.WriteString(" ")
		ctx.FormatNode(&node.AsOf)
	}
	if node.IncrementalFrom != nil {
		ctx.WriteString(" INCREMENTAL FROM ")
		ctx.FormatNode(&node.IncrementalFrom)
	}

	if !node.Options.IsDefault() {
		ctx.WriteString(" WITH ")
		ctx.FormatNode(&node.Options)
	}
}

// Coverage return the coverage (all vs requested).
func (node Backup) Coverage() DescriptorCoverage {
	if node.Targets == nil {
		return AllDescriptors
	}
	return RequestedDescriptors
}

// Restore represents a RESTORE statement.
type Restore struct {
	Targets            TargetList
	DescriptorCoverage DescriptorCoverage
	From               []StringOrPlaceholderOptList
	AsOf               AsOfClause
	Options            KVOptions
	Subdir             Expr
}

var _ Statement = &Restore{}

// Format implements the NodeFormatter interface.
func (node *Restore) Format(ctx *FmtCtx) {
	ctx.WriteString("RESTORE ")
	if node.DescriptorCoverage == RequestedDescriptors {
		ctx.FormatNode(&node.Targets)
		ctx.WriteString(" ")
	}
	ctx.WriteString("FROM ")
	if node.Subdir != nil {
		ctx.FormatNode(node.Subdir)
		ctx.WriteString(" IN ")
	}
	for i := range node.From {
		if i > 0 {
			ctx.WriteString(", ")
		}
		ctx.FormatNode(&node.From[i])
	}
	if node.AsOf.Expr != nil {
		ctx.WriteString(" ")
		ctx.FormatNode(&node.AsOf)
	}
	if node.Options != nil {
		ctx.WriteString(" WITH ")
		ctx.FormatNode(&node.Options)
	}
}

// KVOption is a key-value option.
type KVOption struct {
	Key   Name
	Value Expr
}

// KVOptions is a list of KVOptions.
type KVOptions []KVOption

// Format implements the NodeFormatter interface.
func (o *KVOptions) Format(ctx *FmtCtx) {
	for i := range *o {
		n := &(*o)[i]
		if i > 0 {
			ctx.WriteString(", ")
		}
		ctx.FormatNode(&n.Key)
		if n.Value != nil {
			ctx.WriteString(` = `)
			ctx.FormatNode(n.Value)
		}
	}
}

// StringOrPlaceholderOptList is a list of strings or placeholders.
type StringOrPlaceholderOptList []Expr

// Format implements the NodeFormatter interface.
func (node *StringOrPlaceholderOptList) Format(ctx *FmtCtx) {
	if len(*node) > 1 {
		ctx.WriteString("(")
	}
	ctx.FormatNode((*Exprs)(node))
	if len(*node) > 1 {
		ctx.WriteString(")")
	}
}

// Format implements the NodeFormatter interface
func (o *BackupOptions) Format(ctx *FmtCtx) {
	var addSep bool
	maybeAddSep := func() {
		if addSep {
			ctx.WriteString(", ")
		}
		addSep = true
	}
	if o.CaptureRevisionHistory {
		ctx.WriteString("revision_history")
		addSep = true
	}

	if o.EncryptionPassphrase != nil {
		maybeAddSep()
		ctx.WriteString("encryption_passphrase=")
		o.EncryptionPassphrase.Format(ctx)
	}

	if o.Detached {
		maybeAddSep()
		ctx.WriteString("detached")
	}

	if o.EncryptionKMSURI != nil {
		maybeAddSep()
		ctx.WriteString("kms=")
		o.EncryptionKMSURI.Format(ctx)
	}
}

// CombineWith merges other backup options into this backup options struct.
// An error is returned if the same option merged multiple times.
func (o *BackupOptions) CombineWith(other *BackupOptions) error {
	if o.CaptureRevisionHistory {
		if other.CaptureRevisionHistory {
			return errors.New("revision_history option specified multiple times")
		}
	} else {
		o.CaptureRevisionHistory = other.CaptureRevisionHistory
	}

	if o.EncryptionPassphrase == nil {
		o.EncryptionPassphrase = other.EncryptionPassphrase
	} else if other.EncryptionPassphrase != nil {
		return errors.New("encryption_passphrase specified multiple times")
	}

	if o.Detached {
		if other.Detached {
			return errors.New("detached option specified multiple times")
		}
	} else {
		o.Detached = other.Detached
	}

	if o.EncryptionKMSURI == nil {
		o.EncryptionKMSURI = other.EncryptionKMSURI
	} else if other.EncryptionKMSURI != nil {
		return errors.New("kms specified multiple times")
	}

	return nil
}

// IsDefault returns true if this backup options struct has default value.
func (o BackupOptions) IsDefault() bool {
	options := BackupOptions{}
	return o.CaptureRevisionHistory == options.CaptureRevisionHistory &&
		o.Detached == options.Detached && cmp.Equal(o.EncryptionKMSURI, options.EncryptionKMSURI) &&
		o.EncryptionPassphrase == options.EncryptionPassphrase
}
