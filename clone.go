package sentry

import (
	"bytes"
	"maps"
)

func cloneUser(user User) User {
	user.Data = maps.Clone(user.Data)
	return user
}

func cloneContexts(contexts map[string]Context) map[string]Context {
	if contexts == nil {
		return nil
	}
	clone := make(map[string]Context, len(contexts))
	for key, value := range contexts {
		clone[key] = cloneContext(value)
	}
	return clone
}

func cloneAttachment(attachment *Attachment) *Attachment {
	if attachment == nil {
		return nil
	}
	clone := *attachment
	clone.Payload = bytes.Clone(attachment.Payload)
	return &clone
}

func cloneAttachments(attachments []*Attachment) []*Attachment {
	if attachments == nil {
		return nil
	}
	clone := make([]*Attachment, len(attachments))
	for i, attachment := range attachments {
		clone[i] = cloneAttachment(attachment)
	}
	return clone
}

func cloneBreadcrumb(breadcrumb *Breadcrumb) *Breadcrumb {
	if breadcrumb == nil {
		return nil
	}
	clone := *breadcrumb
	clone.Data = maps.Clone(breadcrumb.Data)
	return &clone
}

func cloneBreadcrumbs(breadcrumbs []*Breadcrumb) []*Breadcrumb {
	if breadcrumbs == nil {
		return nil
	}
	clone := make([]*Breadcrumb, len(breadcrumbs))
	for i, breadcrumb := range breadcrumbs {
		clone[i] = cloneBreadcrumb(breadcrumb)
	}
	return clone
}
