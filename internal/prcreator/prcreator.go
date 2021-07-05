package prcreator

import (
	"bytes"
	"context"
	"fmt"
	"github.com/cresta/gitops-autobot/internal/gogitwrap"
	"github.com/cresta/zapctx"
)

type PrCreator struct {
	repo string
	g *gogitwrap.GitCheckout
	changerFactory []ChangerFactory
	log *zapctx.Logger
}

type ChangedGroup struct {
	Changer Changer
	ChangeRequest []*FileChange
}

type FileChange struct {
	ChangeRequest *ChangeRequest
	File File
}

func (P *PrCreator) Run(ctx context.Context) error {
	if err := P.CheckoutOrUpdateRepo(ctx); err != nil {
		return fmt.Errorf("unable to checkout or update repo: %w", err)
	}
	files, err := P.g.EveryFile(ctx)
	if err != nil {
		return fmt.Errorf("unable to iterate every file: %w", err)
	}
	cfgFile, err := P.FindConfigFile(ctx, files)
	if err != nil {
		return fmt.Errorf("unable to find config file")
	}
	if cfgFile == nil {
		P.log.Info(ctx, "no config file found")
		return nil
	}
	var buf bytes.Buffer
	if _, err := cfgFile.WriteTo(&buf); err != nil {
		return fmt.Errorf("unable to read config file: %w", err)
	}
	var changers []Changer
	for _, f := range P.changerFactory {
		toAdd, err := f.Changers(ctx, buf.Bytes())
		if err != nil {
			return fmt.Errorf("unable to make changers from config file: %w", err)
		}
		changers = append(changers, toAdd...)
	}
	allChanges := make(map[string][]ChangedGroup)

	for _, c := range changers {
		cg := make(map[string][]*FileChange)
		for _, file := range files {
			cr, err := c.ChangeFile(ctx, file)
			if err != nil {
				return fmt.Errorf("unable to make change request for file=%s changer=%s: %w", file.Name(), c.Desc(), err)
			}
			if cr == nil {
				P.log.Debug(ctx, "No change for this file")
				continue
			}
			cg[cr.GroupHash] = append(cg[cr.GroupHash], &FileChange{
				ChangeRequest: cr,
				File:          file,
			})
		}
		for k,v := range cg {
			allChanges[k] = append(allChanges[k], ChangedGroup{
				Changer:       c,
				ChangeRequest: v,
			})
		}
	}
	for k, v := range allChanges {
		// if k is empty, these are all independent PRs
		if k == "" {
			for _, cg := range v {
				for _, cr := range cg.ChangeRequest {
					allChanges := []*FileChange{cr}
					msg, err := cg.Changer.CommitMessage(ctx, allChanges)
					if err != nil {
						return fmt.Errorf("unable to make commit message for changer=%s: %w", cg.Changer.Desc(), err)
					}
					if err := P.CreateAndSendPr(ctx, msg, allChanges); err != nil {
						return fmt.Errorf("unable to send pr: %w", err)
					}
				}
			}
			continue
		}
		var allChanges []*FileChange
		for _, cg := range v {
			allChanges = append(allChanges, cg.ChangeRequest...)
		}
		cmtMsg := ""
		for _, cg := range v {
			msg, err := cg.Changer.CommitMessage(ctx, allChanges)
			if err != nil {
				return fmt.Errorf("unable to get commit message for changes: %w", err)
			}
			if cmtMsg != "" {
				cmtMsg += "\n\n"
			}
			cmtMsg += msg
		}
		if err := P.CreateAndSendPr(ctx, cmtMsg, allChanges); err != nil {
			return fmt.Errorf("unable to send pr: %w", err)
		}
	}
	return nil
}

func (P *PrCreator) CreateAndSendPr(ctx context.Context, msg string, changes []*FileChange) error {
	return nil
}

func (P *PrCreator) FindConfigFile(_ context.Context, files []File) (File, error) {
	for _, file := range files {
		if file.Name() == ".gitops-autobot" {
			return file, nil
		}
	}
	return nil, nil
}

func (P *PrCreator) CheckoutOrUpdateRepo(_ context.Context) error {
	return nil
}
