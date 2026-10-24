// Package authz enthält Rollen, Rechte und deren Auflösung.
//
// Modell: Rechte hängen nur an Rollen. Benutzer sind direkte Mitglieder von Rollen.
// Eine Rolle kann in einer anderen liegen (child in parent): Mitglieder von child sind damit
// auch Mitglieder von parent und erben dessen Rechte. Rechte addieren sich nur, es gibt kein Verbieten.
package authz

import (
	"errors"
	"sort"
)

// Rechte. Neue Rechte werden hier ergänzt, die Datenbank speichert nur den Code.
const (
	PermUsersManage   = "users.manage"
	PermRolesManage   = "roles.manage"
	PermTablesDefine  = "tables.define"
	PermWidgetsManage = "widgets.manage"
)

// Permission beschreibt ein Recht für die Oberfläche.
type Permission struct {
	Code  string
	Label string
}

// AllPermissions in der Reihenfolge, in der sie angezeigt werden.
var AllPermissions = []Permission{
	{PermUsersManage, "Benutzer verwalten"},
	{PermRolesManage, "Rollen und Rechte verwalten"},
	{PermTablesDefine, "Tabellen definieren"},
	{PermWidgetsManage, "Widgets verwalten"},
}

// PermissionLabel liefert den Anzeigenamen eines Rechts.
func PermissionLabel(code string) string {
	for _, p := range AllPermissions {
		if p.Code == code {
			return p.Label
		}
	}
	return code
}

// MaxDepth ist die maximale Zahl verschachtelter Ebenen (A enthält B enthält C).
const MaxDepth = 3

var (
	ErrSelfInclude = errors.New("Eine Rolle kann nicht in sich selbst liegen.")
	ErrCycle       = errors.New("Das würde einen Kreis ergeben: Die Rolle liegt bereits (indirekt) in der anderen.")
	ErrTooDeep     = errors.New("Das würde mehr als 3 Ebenen Verschachtelung ergeben.")
)

// RoleInfo ist das, was die Auflösung über eine Rolle wissen muss.
type RoleInfo struct {
	ID          int64
	Name        string
	System      bool
	Permissions []string
}

// Graph ist ein Abbild aller Rollen und ihrer Verschachtelung.
type Graph struct {
	Roles   map[int64]*RoleInfo
	parents map[int64][]int64 // child -> parents
	childs  map[int64][]int64 // parent -> children
}

func NewGraph(roles []*RoleInfo, includes [][2]int64) *Graph {
	g := &Graph{Roles: map[int64]*RoleInfo{}, parents: map[int64][]int64{}, childs: map[int64][]int64{}}
	for _, r := range roles {
		g.Roles[r.ID] = r
	}
	for _, e := range includes {
		parent, child := e[0], e[1]
		g.parents[child] = append(g.parents[child], parent)
		g.childs[parent] = append(g.childs[parent], child)
	}
	return g
}

// Parents liefert die Rollen, in denen id direkt liegt.
func (g *Graph) Parents(id int64) []int64 { return g.parents[id] }

// Children liefert die Rollen, die direkt in id liegen.
func (g *Graph) Children(id int64) []int64 { return g.childs[id] }

// longest liefert die längste Kette von Kanten ab id in Richtung next.
func longest(id int64, next map[int64][]int64, seen map[int64]bool) int {
	if seen[id] {
		return 0
	}
	seen[id] = true
	defer delete(seen, id)
	best := 0
	for _, n := range next[id] {
		if d := 1 + longest(n, next, seen); d > best {
			best = d
		}
	}
	return best
}

// reaches meldet, ob von from aus in Richtung next to erreichbar ist (oder from == to).
func reaches(from, to int64, next map[int64][]int64) bool {
	stack, seen := []int64{from}, map[int64]bool{}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == to {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, next[id]...)
	}
	return false
}

// CheckInclude prüft, ob child in parent gelegt werden darf.
func (g *Graph) CheckInclude(parent, child int64) error {
	if parent == child {
		return ErrSelfInclude
	}
	// Kreis, wenn child schon über parent liegt, also parent (indirekt) in child steckt.
	if reaches(parent, child, g.parents) {
		return ErrCycle
	}
	levels := longest(parent, g.parents, map[int64]bool{}) + 1 + longest(child, g.childs, map[int64]bool{}) + 1
	if levels > MaxDepth {
		return ErrTooDeep
	}
	return nil
}

// Access ist das Ergebnis der Auflösung für einen Benutzer.
type Access struct {
	// Roles: alle wirksamen Rollen mit dem Pfad, über den der Benutzer Mitglied ist.
	// Pfad[0] ist eine direkte Rolle, das letzte Element die Rolle selbst.
	Roles map[int64][]int64
	// Permissions: Recht -> Pfad zu einer Rolle, die es gewährt (kürzester Pfad).
	Permissions map[string][]int64
}

// Has meldet, ob das Recht vorhanden ist.
func (a *Access) Has(perm string) bool {
	if a == nil {
		return false
	}
	_, ok := a.Permissions[perm]
	return ok
}

// IsAdmin meldet, ob der Benutzer (direkt oder indirekt) in einer Systemrolle ist.
func (a *Access) IsAdmin(g *Graph) bool {
	for id := range a.Roles {
		if r := g.Roles[id]; r != nil && r.System {
			return true
		}
	}
	return false
}

// Resolve ermittelt wirksame Rollen und Rechte aus den direkten Rollen eines Benutzers.
// Breitensuche nach oben, daher ist jeder Pfad der kürzeste.
func (g *Graph) Resolve(direct []int64) *Access {
	a := &Access{Roles: map[int64][]int64{}, Permissions: map[string][]int64{}}
	sorted := append([]int64(nil), direct...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	queue := [][]int64{}
	for _, id := range sorted {
		if _, ok := g.Roles[id]; ok {
			queue = append(queue, []int64{id})
		}
	}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		id := path[len(path)-1]
		if _, seen := a.Roles[id]; seen {
			continue
		}
		a.Roles[id] = path
		role := g.Roles[id]
		perms := role.Permissions
		if role.System {
			perms = nil
			for _, p := range AllPermissions {
				perms = append(perms, p.Code)
			}
		}
		for _, p := range perms {
			if _, ok := a.Permissions[p]; !ok {
				a.Permissions[p] = path
			}
		}
		for _, parent := range g.parents[id] {
			next := append(append([]int64(nil), path...), parent)
			queue = append(queue, next)
		}
	}
	return a
}

// PathNames übersetzt einen Pfad in Rollennamen.
func (g *Graph) PathNames(path []int64) []string {
	names := make([]string, 0, len(path))
	for _, id := range path {
		if r := g.Roles[id]; r != nil {
			names = append(names, r.Name)
		}
	}
	return names
}
