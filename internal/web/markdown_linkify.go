//go:build !story

package web

import (
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/text"
)

// linkifyIssueRefs rewrites textual #N references into links within the
// current workspace. It operates on parsed Text nodes so references inside
// code spans/blocks and existing Markdown links stay plain text.
func linkifyIssueRefs(doc any, source []byte, workspace string) {
	var nodes []*ast.Text
	_ = ast.Walk(doc.(ast.Node), func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		node, ok := n.(*ast.Text)
		if !ok || !issueRefPattern.MatchString(string(node.Value.Value(source))) {
			return ast.WalkContinue, nil
		}
		for p := n.Parent(); p != nil; p = p.Parent() {
			if p.Kind() == ast.KindLink {
				return ast.WalkContinue, nil
			}
		}
		nodes = append(nodes, node)
		return ast.WalkContinue, nil
	})
	for _, node := range nodes {
		value := string(node.Value.Value(source))
		matches := issueRefPattern.FindAllStringSubmatchIndex(value, -1)
		if len(matches) == 0 {
			continue
		}
		parent := node.Parent()
		if parent == nil {
			continue
		}
		pos := 0
		var last *ast.Text
		insertText := func(s string) {
			if s == "" {
				return
			}
			t := ast.NewText(text.NewSingleLineValueFromString(s, nil))
			parent.InsertBefore(node, t)
			last = t
		}
		for _, m := range matches {
			insertText(value[pos:m[0]])
			insertText(value[m[2]:m[3]])
			num := value[m[4]:m[5]]
			label := "#" + num
			link := ast.NewText(text.NewSingleLineValueFromString(label, nil))
			a := &ast.Link{Destination: text.NewSingleLineValueFromString("/w/"+workspace+"/issues/"+num, nil)}
			a.AppendChild(link)
			parent.InsertBefore(node, a)
			last = nil
			pos = m[1]
		}
		insertText(value[pos:])
		if node.SoftLineBreak() || node.HardLineBreak() {
			if last == nil {
				last = ast.NewText(text.NewSingleLineValueFromString("", nil))
				parent.InsertBefore(node, last)
			}
			last.SetSoftLineBreak(node.SoftLineBreak())
			last.SetHardLineBreak(node.HardLineBreak())
		}
		parent.RemoveChild(node)
	}
}
