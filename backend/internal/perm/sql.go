package perm

import "fmt"

// ViewablePage is a SQL condition true when the actor, bound as parameter
// $actorParam, may view the page aliased alias; the trash is the caller's.
//
// It is the one rule for every list of pages: membership, use, the space's
// view, unpublished pages of others and everything below them, and view
// restrictions inherited down the tree, which space administrators pass.
func ViewablePage(alias string, actorParam int) string {
	return fmt.Sprintf("perm_page_viewable(%s.id, $%d::uuid)", alias, actorParam)
}

// ViewableSpace is a SQL condition true when the actor, bound as parameter
// $actorParam, may view the space aliased alias.
func ViewableSpace(alias string, actorParam int) string {
	return fmt.Sprintf("perm_space_holds($%d::uuid, %s.id, 'view')", actorParam, alias)
}
