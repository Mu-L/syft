package source

import (
	"fmt"
	"io"

	"github.com/anchore/stereoscope/pkg/file"
	"github.com/anchore/stereoscope/pkg/filetree"
	"github.com/anchore/stereoscope/pkg/image"
)

var _ FileResolver = (*imageSquashResolver)(nil)

// imageSquashResolver implements path and content access for the Squashed source option for container image data sources.
type imageSquashResolver struct {
	img  *image.Image
	refs file.ReferenceSet
}

// newImageSquashResolver returns a new resolver from the perspective of the squashed representation for the given image.
func newImageSquashResolver(img *image.Image) (*imageSquashResolver, error) {
	if img.SquashedTree() == nil {
		return nil, fmt.Errorf("the image does not have have a squashed tree")
	}

	refs := file.NewFileReferenceSet()
	for _, r := range img.SquashedTree().AllFiles() {
		refs.Add(r)
	}

	return &imageSquashResolver{
		img:  img,
		refs: refs,
	}, nil
}

func (r *imageSquashResolver) HasLocation(l Location) bool {
	if l.ref.ID() == 0 {
		return false
	}
	return r.refs.Contains(l.ref)
}

// HasPath indicates if the given path exists in the underlying source.
func (r *imageSquashResolver) HasPath(path string) bool {
	return r.img.SquashedTree().HasPath(file.Path(path))
}

// FilesByPath returns all file.References that match the given paths within the squashed representation of the image.
func (r *imageSquashResolver) FilesByPath(paths ...string) ([]Location, error) {
	uniqueFileIDs := file.NewFileReferenceSet()
	uniqueLocations := make([]Location, 0)

	for _, path := range paths {
		tree := r.img.SquashedTree()
		_, ref, err := tree.File(file.Path(path), filetree.FollowBasenameLinks)
		if err != nil {
			return nil, err
		}
		if ref == nil {
			// no file found, keep looking through layers
			continue
		}

		// don't consider directories (special case: there is no path information for /)
		if ref.RealPath == "/" {
			continue
		} else if r.img.FileCatalog.Exists(*ref) {
			metadata, err := r.img.FileCatalog.Get(*ref)
			if err != nil {
				return nil, fmt.Errorf("unable to get file metadata for path=%q: %w", ref.RealPath, err)
			}
			if metadata.Metadata.IsDir {
				continue
			}
		}

		// a file may be a symlink, process it as such and resolve it
		resolvedRef, err := r.img.ResolveLinkByImageSquash(*ref)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve link from img (ref=%+v): %w", ref, err)
		}

		if resolvedRef != nil && !uniqueFileIDs.Contains(*resolvedRef) {
			uniqueFileIDs.Add(*resolvedRef)
			uniqueLocations = append(uniqueLocations, NewLocationFromImage(path, *resolvedRef, r.img))
		}
	}

	return uniqueLocations, nil
}

// FilesByGlob returns all file.References that match the given path glob pattern within the squashed representation of the image.
func (r *imageSquashResolver) FilesByGlob(patterns ...string) ([]Location, error) {
	uniqueFileIDs := file.NewFileReferenceSet()
	uniqueLocations := make([]Location, 0)

	for _, pattern := range patterns {
		results, err := r.img.SquashedTree().FilesByGlob(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve files by glob (%s): %w", pattern, err)
		}

		for _, result := range results {
			// don't consider directories (special case: there is no path information for /)
			if result.MatchPath == "/" {
				continue
			} else if r.img.FileCatalog.Exists(result.Reference) {
				metadata, err := r.img.FileCatalog.Get(result.Reference)
				if err != nil {
					return nil, fmt.Errorf("unable to get file metadata for path=%q: %w", result.MatchPath, err)
				}
				if metadata.Metadata.IsDir {
					continue
				}
			}

			resolvedLocations, err := r.FilesByPath(string(result.MatchPath))
			if err != nil {
				return nil, fmt.Errorf("failed to find files by path (result=%+v): %w", result, err)
			}
			for _, resolvedLocation := range resolvedLocations {
				if !uniqueFileIDs.Contains(resolvedLocation.ref) {
					uniqueFileIDs.Add(resolvedLocation.ref)
					uniqueLocations = append(uniqueLocations, resolvedLocation)
				}
			}
		}
	}

	return uniqueLocations, nil
}

// RelativeFileByPath fetches a single file at the given path relative to the layer squash of the given reference.
// This is helpful when attempting to find a file that is in the same layer or lower as another file. For the
// imageSquashResolver, this is a simple path lookup.
func (r *imageSquashResolver) RelativeFileByPath(_ Location, path string) *Location {
	paths, err := r.FilesByPath(path)
	if err != nil {
		return nil
	}
	if len(paths) == 0 {
		return nil
	}

	return &paths[0]
}

// MultipleFileContentsByLocation returns the file contents for all file.References relative to the image. Note that a
// file.Reference is a path relative to a particular layer, in this case only from the squashed representation.
func (r *imageSquashResolver) MultipleFileContentsByLocation(locations []Location) (map[Location]io.ReadCloser, error) {
	return mapLocationRefs(r.img.MultipleFileContentsByRef, locations)
}

// FileContentsByLocation fetches file contents for a single file reference, irregardless of the source layer.
// If the path does not exist an error is returned.
func (r *imageSquashResolver) FileContentsByLocation(location Location) (io.ReadCloser, error) {
	return r.img.FileContentsByRef(location.ref)
}