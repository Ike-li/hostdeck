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

# jobs_parse_labels reads one launchctl domain listing on stdin.
jobs_parse_labels() {
	awk '
		/services = \{/ { inside = 1; next }
		inside && /^[[:space:]]*\}/ { closed = 1; exit }
		inside && NF >= 1 { print $NF }
		END { if (!closed) exit 1 }
	'
}

jobs_labels() {
	listing=$(launchctl print "$1" 2>/dev/null) || return 1
	printf '%s\n' "$listing" | jobs_parse_labels
}

# jobs_open prints the domain once and keeps it in jobs_listing. The name is
# jobs_domain_name. A second print of the same domain is what made the page wait.
jobs_open() {
	uid=$(id -u)
	if jobs_listing=$(launchctl print "gui/$uid" 2>/dev/null); then
		jobs_domain_name="gui/$uid"
		return 0
	fi
	if jobs_listing=$(launchctl print "user/$uid" 2>/dev/null); then
		jobs_domain_name="user/$uid"
		return 0
	fi
	return 1
}

jobs_error() {
	printf '{"error":{"en":"%s","zh-CN":"%s"}}\n' "$(jobs_escape "$1")" "$(jobs_escape "$2")"
	exit 0
}

jobs_escape_awk() {
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

# macOS /bin/sh is Bash 3.2. Keep short JSON strings in the shell instead of
# starting awk for every field. Large log bodies still use the streaming encoder.
jobs_escape() {
 if [ -z "${BASH_VERSION-}" ] || [ "${#1}" -gt 8192 ]; then
  jobs_escape_awk "$1"
  return
 fi
 local text=$1
 text=${text//\\/\\\\}
 text=${text//\"/\\\"}
 text=${text//$'\001'/\\u0001}
 text=${text//$'\002'/\\u0002}
 text=${text//$'\003'/\\u0003}
 text=${text//$'\004'/\\u0004}
 text=${text//$'\005'/\\u0005}
 text=${text//$'\006'/\\u0006}
 text=${text//$'\007'/\\u0007}
 text=${text//$'\010'/\\u0008}
 text=${text//$'\011'/\\u0009}
 text=${text//$'\012'/\\u000a}
 text=${text//$'\013'/\\u000b}
 text=${text//$'\014'/\\u000c}
 text=${text//$'\015'/\\u000d}
 text=${text//$'\016'/\\u000e}
 text=${text//$'\017'/\\u000f}
 text=${text//$'\020'/\\u0010}
 text=${text//$'\021'/\\u0011}
 text=${text//$'\022'/\\u0012}
 text=${text//$'\023'/\\u0013}
 text=${text//$'\024'/\\u0014}
 text=${text//$'\025'/\\u0015}
 text=${text//$'\026'/\\u0016}
 text=${text//$'\027'/\\u0017}
 text=${text//$'\030'/\\u0018}
 text=${text//$'\031'/\\u0019}
 text=${text//$'\032'/\\u001a}
 text=${text//$'\033'/\\u001b}
 text=${text//$'\034'/\\u001c}
 text=${text//$'\035'/\\u001d}
 text=${text//$'\036'/\\u001e}
 text=${text//$'\037'/\\u001f}
 printf '%s' "$text"
}

# jobs_apply reads one launchctl job listing already in $1.
# It sets jobs_state, jobs_code, jobs_signal, jobs_program, jobs_runs,
# jobs_path, jobs_pid, jobs_stdout and jobs_stderr.
jobs_apply() {
 jobs_path= jobs_pid= jobs_stderr= jobs_state= jobs_code= jobs_signal=
 jobs_program= jobs_runs= jobs_stdout=
 local line
 while IFS= read -r line; do
  line=${line#"${line%%[![:space:]]*}"}
  case "$line" in
   'path = '*) [ -n "$jobs_path" ] || jobs_path=${line#'path = '} ;;
   'pid = '*) [ -n "$jobs_pid" ] || jobs_pid=${line#'pid = '} ;;
   'stderr path = '*) [ -n "$jobs_stderr" ] || jobs_stderr=${line#'stderr path = '} ;;
   'state = '*) [ -n "$jobs_state" ] || jobs_state=${line#'state = '} ;;
   'last exit code = '*) [ -n "$jobs_code" ] || jobs_code=${line#'last exit code = '} ;;
   'last terminating signal = '*) [ -n "$jobs_signal" ] || jobs_signal=${line#'last terminating signal = '} ;;
   'program = '*) [ -n "$jobs_program" ] || jobs_program=${line#'program = '} ;;
   'runs = '*) [ -n "$jobs_runs" ] || jobs_runs=${line#'runs = '} ;;
   'stdout path = '*) [ -n "$jobs_stdout" ] || jobs_stdout=${line#'stdout path = '} ;;
  esac
 done <<EOF
$1
EOF
}

# jobs_read sets the same variables as jobs_apply, or stops with an error.
jobs_read() {
	if ! info=$(launchctl print "$1/$2" 2>/dev/null); then
		jobs_error "Could not read launchd job: $2" "读不到这条 launchd 任务：$2"
	fi
	jobs_apply "$info"
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
	# Explicit characters avoid locale-dependent ranges accepting accented letters.
	case "$1" in
	""|*[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-]*) return 1 ;;
	*) return 0 ;;
	esac
}
