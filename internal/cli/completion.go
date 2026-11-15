// `issues completion <shell>` — print a static completion script, like
// `gh completion`. The grammar is fixed (no dynamic subcommands), so the
// scripts are literals rather than generated.
package cli

import (
	"context"
	"flag"
	"fmt"
)

// issues completion bash|zsh|fish — print the completion script for the
// shell; the user pipes it to their shell config.
func completionCmd(ctx context.Context, o Options, args []string) error {
	_ = ctx // completions are static; nothing to fetch
	fs := flag.NewFlagSet("completion", flag.ContinueOnError)
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return usageError{msg: "usage: issues completion bash|zsh|fish"}
	}
	switch rest[0] {
	case "bash":
		fmt.Fprint(o.Stdout, completionBash)
	case "zsh":
		fmt.Fprint(o.Stdout, completionZsh)
	case "fish":
		fmt.Fprint(o.Stdout, completionFish)
	default:
		return usageError{msg: fmt.Sprintf("unknown shell %q (bash|zsh|fish)", rest[0])}
	}
	return nil
}

const completionBash = `# bash completion for issues — eval "$(issues completion bash)" or save to
# /etc/bash_completion.d/issues
_issues_completions() {
	local cur prev
	cur="${COMP_WORDS[COMP_CWORD]}"
	prev="${COMP_WORDS[COMP_CWORD-1]}"

	if [ "$COMP_CWORD" -eq 1 ]; then
		COMPREPLY=( $(compgen -W "issue comment label discussion auth api completion help" -- "$cur") )
		return 0
	fi

	# Flag words complete anywhere; values of flags that take one are left
	# to the shell's file completion.
	if [[ "$cur" == -* ]]; then
		COMPREPLY=( $(compgen -W "--db --server --token --json --template --state --search --sort --limit --page --label --category --answered --title --body --body-file --comments --parent --comment --unmark --user --method --field --raw-field --web" -- "$cur") )
		return 0
	fi

	case "${COMP_WORDS[1]}" in
	issue)
		if [ "$COMP_CWORD" -eq 2 ]; then
			COMPREPLY=( $(compgen -W "list view create edit close reopen delete" -- "$cur") )
		fi
		;;
	comment)
		if [ "$COMP_CWORD" -eq 2 ]; then
			COMPREPLY=( $(compgen -W "create" -- "$cur") )
		fi
		;;
	label)
		if [ "$COMP_CWORD" -eq 2 ]; then
			COMPREPLY=( $(compgen -W "list" -- "$cur") )
		fi
		;;
	discussion)
		if [ "$COMP_CWORD" -eq 2 ]; then
			COMPREPLY=( $(compgen -W "list view create edit delete comment answer" -- "$cur") )
		elif [ "${COMP_WORDS[2]}" = "comment" ] && [ "$COMP_CWORD" -eq 3 ]; then
			COMPREPLY=( $(compgen -W "create" -- "$cur") )
		fi
		;;
	auth)
		if [ "$COMP_CWORD" -eq 2 ]; then
			COMPREPLY=( $(compgen -W "token" -- "$cur") )
		elif [ "$COMP_CWORD" -eq 3 ]; then
			COMPREPLY=( $(compgen -W "list revoke" -- "$cur") )
		fi
		;;
	completion)
		COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") )
		;;
	esac
	if [ "$prev" = "--state" ]; then
		COMPREPLY=( $(compgen -W "open closed all" -- "$cur") )
	elif [ "$prev" = "--sort" ]; then
		COMPREPLY=( $(compgen -W "newest oldest" -- "$cur") )
	elif [ "$prev" = "--answered" ]; then
		COMPREPLY=( $(compgen -W "yes no" -- "$cur") )
	elif [ "$prev" = "--method" ] || [ "$prev" = "-X" ]; then
		COMPREPLY=( $(compgen -W "GET POST PATCH PUT DELETE" -- "$cur") )
	fi
	return 0
}
complete -F _issues_completions issues
`

const completionZsh = `#compdef issues
# zsh completion for issues — save as _issues on your fpath, or
# eval "$(issues completion zsh)" with compinit loaded.
_issues() {
	local -a commands
	commands=(
		'issue:issue list|view|create|edit|close|reopen|delete'
		'comment:create an issue comment'
		'label:list labels'
		'discussion:discussion list|view|create|edit|delete|comment|answer'
		'auth:API token management (local)'
		'api:raw call to /api/v1'
		'completion:print a shell completion script'
		'help:show usage'
	)
	if (( CURRENT == 2 )); then
		_describe -t commands 'issues command' commands
		return
	fi
	case "$words[2]" in
		issue)
			_describe -t actions 'issue action' '(
				list view create edit close reopen delete
			)' ;;
		comment)
			_describe -t actions 'comment action' '(create)' ;;
		label)
			_describe -t actions 'label action' '(list)' ;;
		discussion)
			if (( CURRENT == 3 )); then
				_describe -t actions 'discussion action' '(
					list view create edit delete comment answer
				)'
			elif [[ "$words[3]" == comment ]]; then
				_describe -t actions 'comment action' '(create)'
			fi ;;
		auth)
			if (( CURRENT == 3 )); then
				_describe -t actions 'auth action' '(token)'
			elif [[ "$words[3]" == token ]]; then
				_describe -t actions 'token action' '(list revoke)'
			fi ;;
		completion)
			_describe -t shells 'shell' '(bash zsh fish)' ;;
	esac
}
_issues "$@"
`

const completionFish = `# fish completion for issues — save to ~/.config/fish/completions/issues.fish
# or: issues completion fish | source
complete -c issues -f
complete -c issues -n '__fish_use_subcommand' -a 'issue comment label discussion auth api completion help'
complete -c issues -n '__fish_seen_subcommand_from issue' -a 'list view create edit close reopen delete'
complete -c issues -n '__fish_seen_subcommand_from comment' -a 'create'
complete -c issues -n '__fish_seen_subcommand_from label' -a 'list'
complete -c issues -n '__fish_seen_subcommand_from discussion' -a 'list view create edit delete comment answer'
complete -c issues -n '__fish_seen_subcommand_from discussion; and __fish_seen_subcommand_from comment' -a 'create'
complete -c issues -n '__fish_seen_subcommand_from auth' -a 'token'
complete -c issues -n '__fish_seen_subcommand_from auth; and __fish_seen_subcommand_from token' -a 'list revoke'
complete -c issues -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
complete -c issues -l state -x -a 'open closed all'
complete -c issues -l sort -x -a 'newest oldest'
complete -c issues -l answered -x -a 'yes no'
complete -c issues -l method -s X -x -a 'GET POST PATCH PUT DELETE'
`
