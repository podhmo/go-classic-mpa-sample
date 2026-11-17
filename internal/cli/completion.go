// `zatu completion <shell>` — print a static completion script, like
// `gh completion`. The grammar is fixed (no dynamic subcommands), so the
// scripts are literals rather than generated.
package cli

import (
	"context"
	"flag"
	"fmt"
)

// zatu completion bash|zsh|fish — print the completion script for the
// shell; the user pipes it to their shell config.
func completionCmd(ctx context.Context, o Options, args []string) error {
	_ = ctx // completions are static; nothing to fetch
	fs := flag.NewFlagSet("completion", flag.ContinueOnError)
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return usageError{msg: "usage: zatu completion bash|zsh|fish"}
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

const completionBash = `# bash completion for zatu — eval "$(zatu completion bash)" or save to
# /etc/bash_completion.d/zatu
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
		COMPREPLY=( $(compgen -W "--db --server --token --json --template --state --search --sort --limit --page --label --category --answered --title --body --body-file --comments --thread --unmark --user --method --field --raw-field --web" -- "$cur") )
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
			COMPREPLY=( $(compgen -W "list create delete" -- "$cur") )
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
complete -F _issues_completions zatu
`

const completionZsh = `#compdef zatu
# zsh completion for zatu — save as _zatu on your fpath, or
# eval "$(zatu completion zsh)" with compinit loaded.
_zatu() {
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
		_describe -t commands 'zatu command' commands
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
			_describe -t actions 'label action' '(list create delete)' ;;
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
# Registering via compdef (not just defining _zatu) is what makes both
# eval "$(zatu completion zsh)" and an fpath-installed _zatu file
# actually hook the command into the completion system.
compdef _zatu zatu
`

const completionFish = `# fish completion for zatu — save to ~/.config/fish/completions/zatu.fish
# or: zatu completion fish | source
complete -c zatu -f
complete -c zatu -n '__fish_use_subcommand' -a 'issue comment label discussion auth api completion help'
complete -c zatu -n '__fish_seen_subcommand_from issue' -a 'list view create edit close reopen delete'
complete -c zatu -n '__fish_seen_subcommand_from comment' -a 'create'
complete -c zatu -n '__fish_seen_subcommand_from label' -a 'list create delete'
complete -c zatu -n '__fish_seen_subcommand_from discussion' -a 'list view create edit delete comment answer'
complete -c zatu -n '__fish_seen_subcommand_from discussion; and __fish_seen_subcommand_from comment' -a 'create'
complete -c zatu -n '__fish_seen_subcommand_from auth' -a 'token'
complete -c zatu -n '__fish_seen_subcommand_from auth; and __fish_seen_subcommand_from token' -a 'list revoke'
complete -c zatu -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
complete -c zatu -l state -x -a 'open closed all'
complete -c zatu -l sort -x -a 'newest oldest'
complete -c zatu -l answered -x -a 'yes no'
complete -c zatu -l method -s X -x -a 'GET POST PATCH PUT DELETE'
`
