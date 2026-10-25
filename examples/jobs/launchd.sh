# Shared by the jobs plugin's card, detail and action scripts. A job is a
# service loaded in the gui domain, or in the user domain when the gui domain
# cannot be printed. Apple's agents and ordinary applications are left out.

jobs_domain() {
	uid=$(id -u)
	if launchctl print "gui/$uid" >/dev/null 2>&1; then
		printf '%s\n' "gui/$uid"
		return 0
	fi
	if launchctl print "user/$uid" >/dev/null 2>&1; then
		printf '%s\n' "user/$uid"
		return 0
	fi
	return 1
}

jobs_labels() {
	listing=$(launchctl print "$1" 2>/dev/null) || return 1
	printf '%s\n' "$listing" | awk '
		/services = \{/ { inside = 1; next }
		inside && /^[[:space:]]*\}/ { closed = 1; exit }
		inside && NF >= 1 { print $NF }
		END { if (!closed) exit 1 }
	'
}

jobs_error() {
	printf '{"error":{"en":"%s","zh-CN":"%s"}}\n' "$(jobs_escape "$1")" "$(jobs_escape "$2")"
	exit 0
}

jobs_escape() {
	# The sentinel preserves trailing newlines through awk's record splitting.
	printf '%s_' "$1" | LC_ALL=C awk '
		BEGIN { for (i = 1; i < 32; i++) escaped[sprintf("%c", i)] = sprintf("\\u%04x", i) }
		{ text = text (NR > 1 ? "\n" : "") $0 }
		END {
			for (i = 1; i < length(text); i++) {
				c = substr(text, i, 1)
				if (c == "\\") printf "\\\\"
				else if (c == "\"") printf "\\\""
				else if (c in escaped) printf "%s", escaped[c]
				else printf "%s", c
			}
		}
	'
}

# jobs_read sets jobs_state, jobs_code, jobs_signal, jobs_program, jobs_runs and jobs_stdout.
jobs_read() {
	if ! info=$(launchctl print "$1/$2" 2>/dev/null); then
		jobs_error "Could not read launchd job: $2" "读不到这条 launchd 任务：$2"
	fi
	jobs_state=$(printf '%s\n' "$info" | sed -n 's/^[[:space:]]*state = //p' | head -n 1)
	jobs_code=$(printf '%s\n' "$info" | sed -n 's/^[[:space:]]*last exit code = //p' | head -n 1)
	jobs_signal=$(printf '%s\n' "$info" | sed -n 's/^[[:space:]]*last terminating signal = //p' | head -n 1)
	jobs_program=$(printf '%s\n' "$info" | sed -n 's/^[[:space:]]*program = //p' | head -n 1)
	jobs_runs=$(printf '%s\n' "$info" | sed -n 's/^[[:space:]]*runs = //p' | head -n 1)
	jobs_stdout=$(printf '%s\n' "$info" | sed -n 's/^[[:space:]]*stdout path = //p' | head -n 1)
}

# jobs_classify sets jobs_status and jobs_value from the last jobs_read.
jobs_classify() {
	termination=
	if [ -n "$jobs_signal" ]; then
		termination="signal $jobs_signal"
	else
		case "$jobs_code" in
		""|0|"(never exited)") ;;
		*) termination="exit $jobs_code" ;;
		esac
	fi
	case "$jobs_state" in
	running)
		jobs_status=ok
		jobs_value=running
		;;
	"not running")
		if [ -z "$termination" ]; then
			jobs_status=ok
			jobs_value=idle
		else
			jobs_status=error
			jobs_value="not running · $termination"
		fi
		;;
	*)
		jobs_status=error
		jobs_value=${jobs_state:-unknown}
		if [ -n "$termination" ]; then
			jobs_value="$jobs_value · $termination"
		fi
		;;
	esac
}

jobs_safe() {
	case "$1" in
	""|*[!A-Za-z0-9._-]*) return 1 ;;
	*) return 0 ;;
	esac
}
