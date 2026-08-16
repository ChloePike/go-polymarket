#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
# Copyright (C) 2026 ChloePike

# Apply .github/labels.yml to the repository.
#
# Creating and renaming only: a label this file does not name is left alone,
# because deleting one takes it off every issue that carries it, and that is
# not recoverable. Print what is extra and decide by hand.

set -eu

repo=${1:-ChloePike/go-polymarket}
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
file=$dir/labels.yml

# The file is a flat list of name/color/description, so awk reads it without
# needing a YAML parser on the machine running this.
awk '
	/^- name:/ { flush(); name = value($0); next }
	/^  color:/ { color = value($0); next }
	/^  description:/ { desc = value($0); next }
	END { flush() }

	function value(line,   v) {
		sub(/^[^:]*: */, "", line)
		gsub(/^"|"$/, "", line)
		return line
	}
	function flush() {
		if (name != "") printf "%s\t%s\t%s\n", name, color, desc
		name = ""; color = ""; desc = ""
	}
' "$file" | while IFS="$(printf '\t')" read -r name color desc; do
	if gh label create "$name" --repo "$repo" --color "$color" --description "$desc" 2>/dev/null; then
		echo "created  $name"
	else
		gh label edit "$name" --repo "$repo" --color "$color" --description "$desc" >/dev/null
		echo "updated  $name"
	fi
done

echo
echo "labels on the repository that this file does not name:"
gh label list --repo "$repo" --limit 200 --json name --jq '.[].name' |
	while read -r name; do
		# -- because a pattern beginning with a dash is otherwise an option,
		# and every name in the file begins with one.
		grep -qxF -- "- name: $name" "$file" ||
			grep -qxF -- "- name: \"$name\"" "$file" ||
			echo "  $name"
	done
