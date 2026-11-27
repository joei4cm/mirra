package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
	"github.com/yldm-ai/mirra/apps/mirra/locales"
)

// An upload is a photo of the user's own clothes, laid flat or on a hanger, kept in the app's uploads folder and used as an outfit.
const (
	uploadGroup  = "upload"
	uploadPrefix = "MY-"
)

// Wardrobe tabs.
const (
	closetFavorites = iota
	closetUploads
	closetGallery
)

// saveUpload keeps a picked photo in dir under the time it was added.
func saveUpload(st pictures, dir string, data []byte, at time.Time) (string, error) {
	return writePicture(st, dir, data, at)
}

// listUploads returns the user's clothes in dir, oldest first, numbered MY-01, MY-02 and on in that order.
func listUploads(st pictures, dir string) ([]Item, error) {
	names, err := st.List(dir)
	if err != nil {
		return nil, err
	}
	type file struct {
		path  string
		nanos int64
	}
	var files []file
	for _, name := range names {
		nanos, err := strconv.ParseInt(strings.TrimSuffix(name, filepath.Ext(name)), 10, 64)
		if err != nil {
			continue
		}
		files = append(files, file{filepath.Join(dir, name), nanos})
	}
	slices.SortFunc(files, func(a, b file) int { return cmp.Compare(a.nanos, b.nanos) })
	out := make([]Item, len(files))
	for i, f := range files {
		out[i] = Item{Kind: KindCostume, ID: fmt.Sprintf("%s%02d", uploadPrefix, i+1), Group: uploadGroup, Thumb: f.path, Full: f.path}
	}
	return out, nil
}

func (s *studio) isFavorite(it Item) bool {
	return slices.ContainsFunc(s.favorites.Get(), func(f Item) bool { return f.ID == it.ID })
}

// toggleFavoriteOutfit keeps an outfit in the wardrobe's favorites, or takes it out.
func (s *studio) toggleFavoriteOutfit(it Item) {
	t := locales.For(s.cx)
	s.favorites.Update(func(fs []Item) []Item {
		if i := slices.IndexFunc(fs, func(f Item) bool { return f.ID == it.ID }); i >= 0 {
			return slices.Delete(slices.Clone(fs), i, i+1)
		}
		return append([]Item{it}, fs...)
	})
	data, err := json.Marshal(s.favorites.Get())
	if err == nil {
		err = s.cx.Storage().SetItem(keyFavorites, string(data))
	}
	if err != nil {
		s.toastError(t.SaveFailed(), err)
	}
}

// uploadClothes asks for a photo of the user's clothes and wears it.
func (s *studio) uploadClothes() {
	t := locales.For(s.cx)
	dir, now, st := s.dir("uploads"), s.cx.Now(), s.pics
	s.cx.PickImages(core.PickOptions{Limit: 1, MaxDimension: 2048}, func(files []core.PickedFile) {
		if len(files) != 1 {
			return
		}
		// The pickers only offer pictures, but a file whose bytes do not decode arrives as it is on the desktops; storing it would put a tile in the wardrobe that can never render, and uploads have no way out.
		if !strings.HasPrefix(files[0].MIME, "image/") {
			s.toaster.Show(ui.ToastContent{Type: ui.ToastError, Title: t.NotAnImage()})
			return
		}
		data := files[0].Data
		go func() {
			_, err := saveUpload(st, dir, data, now)
			var list []Item
			if err == nil {
				list, err = listUploads(st, dir)
			}
			s.cx.Post(func() {
				if err != nil {
					s.toastError(t.SaveFailed(), err)
					return
				}
				s.uploads.Set(list)
				s.closet.Set(closetUploads)
				if len(list) > 0 {
					s.choose(KindCostume, list[len(list)-1])
				}
			})
		}()
	})
}

// wearAndGenerate makes the look with the outfit on, or asks for the pose it still needs.
func (s *studio) wearAndGenerate() {
	t := locales.For(s.cx)
	if s.chosen[KindPose].Get() == nil {
		s.toaster.Show(ui.ToastContent{Type: ui.ToastInfo, Title: t.NeedPose()})
		s.section.Set(sectionStudio)
		s.step.Set(KindPose)
		if !s.wide() {
			s.studioNav.SetRoutes(route{page: pageHome}, route{page: pageEditor})
		}
		return
	}
	s.section.Set(sectionStudio)
	s.generate()
}

// itemGrid is a grid of outfits from a list the app keeps, with empty, a title and a line, shown while it has none.
func (s *studio) itemGrid(cx *core.Scope, list func() []Item, columns int, emptyTitle, emptyBody string) core.Node {
	t := locales.For(cx)
	colors := ui.ColorsOf(cx)
	at := func(i int) (Item, bool) {
		l := list()
		if i < 0 || i >= len(l) {
			return Item{}, false
		}
		return l[i], true
	}
	return core.Show{
		When: func() bool { return len(list()) == 0 },
		Then: func(*core.Scope) core.Node {
			return core.View{Style: fill.Merge(core.Style{AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 8, Padding: core.All(24)}), Children: []core.Node{
				core.Text{Content: emptyTitle, Style: core.Style{FontFamily: displayFont, FontSize: 20, FontWeight: core.FontWeightBold}},
				core.Text{Content: emptyBody, StyleFunc: func() core.Style {
					return core.Style{FontSize: 14, Color: colors.Muted(), TextAlign: core.TextAlignCenter}
				}},
			}}
		},
		Else: func(*core.Scope) core.Node {
			return core.List{Style: fill, NumColumns: columns, Gap: 12, ColumnGap: 12, EstimatedSize: 260,
				Count: func() int { return len(list()) },
				Key: func(i int) any {
					it, _ := at(i)
					return it.ID
				},
				Row: func(cx *core.Scope, row core.ListRow) core.Node {
					item := func() Item {
						it, _ := at(row.Index())
						return it
					}
					on := func() bool {
						c := s.chosen[KindCostume].Get()
						return c != nil && c.ID == item().ID
					}
					sub := func() string {
						if on() {
							return t.Wearing()
						}
						it := item()
						return t.TapToTry(groupLabel(t, it.Group))
					}
					return s.itemCard(cx, KindCostume, item, on, sub, func() {
						if it, ok := at(row.Index()); ok {
							s.choose(KindCostume, it)
						}
					})
				},
			}
		},
	}
}

// wardrobe is the user's favorites, their own clothes and the outfit gallery, with what is being worn at the bottom.
func (s *studio) wardrobe(cx *core.Scope, columns int) core.Node {
	t := locales.For(cx)
	colors := ui.ColorsOf(cx)
	tabs := []ui.Option[int]{
		{Value: closetFavorites, TestID: "closet-favorites", Content: core.Text{ContentFunc: func() string { return t.FavoritesCount(len(s.favorites.Get())) }}, Label: t.Favorites()},
		{Value: closetUploads, TestID: "closet-uploads", Content: core.Text{ContentFunc: func() string { return t.UploadsCount(len(s.uploads.Get())) }}, Label: t.MyUploads()},
		{Value: closetGallery, TestID: "closet-gallery", Content: core.Text{ContentFunc: func() string { return t.GalleryCount(len(loaded(s.catalogs[KindCostume]).Items)) }}, Label: t.OutfitGallery()},
	}
	wearing := s.chosen[KindCostume]
	bar := core.Show{When: func() bool { return wearing.Get() != nil }, Then: func(cx *core.Scope) core.Node {
		return core.View{TestID: "wearing", StyleFunc: func() core.Style {
			return core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Padding: core.All(10), CornerRadius: 999,
				Background: colors.Surface(), BorderWidth: 1, BorderColor: colors.Border()}
		}, Children: []core.Node{
			s.thumb(cx, KindCostume, wearing.Get, 44, 44, true),
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, Gap: 2}, Children: []core.Node{
				caption(cx, t.Wearing),
				core.Text{ContentFunc: func() string { return itemName(t, wearing.Get()) }, MaxLines: 1, Style: core.Style{FontSize: 15, FontWeight: core.FontWeightSemibold}},
			}},
			ui.Button{Text: t.WearAndGenerate(), TestID: "wear-generate", Style: core.Style{CornerRadius: 999}, OnPress: s.wearAndGenerate},
		}}
	}}
	body := core.Dynamic(func(cx *core.Scope) core.Node {
		switch s.closet.Get() {
		case closetFavorites:
			return s.itemGrid(cx, s.favorites.Get, columns, t.NoFavoritesTitle(), t.NoFavoritesBody())
		case closetUploads:
			grid := s.itemGrid(cx, s.uploads.Get, columns, t.NoUploadsTitle(), t.NoUploadsBody())
			if s.pics.Lasting() {
				return grid
			}
			return core.View{Style: fill.Merge(core.Style{Gap: 10}), Children: []core.Node{s.pageOnlyNote(cx), grid}}
		}
		return s.library(cx, KindCostume, columns)
	})
	return core.View{TestID: "wardrobe", Style: fill.Merge(core.Style{Gap: 14}), Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignEnd, JustifyContent: core.JustifySpaceBetween}, Children: []core.Node{
			core.View{Style: core.Style{Gap: 4}, Children: []core.Node{eyebrow(cx, t.EyebrowWardrobe), display(t.TabWardrobe, 30)}},
			ui.Button{Text: t.UploadClothes(), Icon: lucide.Plus, Variant: ui.ButtonOutline, TestID: "upload", Style: core.Style{CornerRadius: 999}, OnPress: s.uploadClothes},
		}},
		ui.Tabs[int]{Label: t.TabWardrobe(), Options: tabs, Value: s.closet.Get, OnChange: s.closet.Set, Equal: true, TestID: "closet"},
		body,
		bar,
	}}
}
