#!/bin/bash
#########################################
# Copyright (c) 2024 ABLECLOUD Co. Ltd.
#
# SCVM 로컬 Samba 구성/조회 스크립트
#########################################
set -Eeuo pipefail

smb_conf="${ABLESTACK_GLUE_SMB_CONF:-/etc/samba/smb.conf}"
smb_state_file="${ABLESTACK_GLUE_SMB_CONF_JSON:-/etc/ablestack/smb.json}"
smb_conf_helper="${ABLESTACK_GLUE_SMB_CONF_HELPER:-/etc/ablestack/shell/smb_conf}"
network_ifcfg="${ABLESTACK_GLUE_SMB_NETWORK_IFCFG:-/etc/sysconfig/network-scripts/ifcfg-enp0s20}"

ERR_USAGE=64
ERR_DEPENDENCY=65
ERR_CONFIG=66
ERR_RUNTIME=70

warn() {
        printf 'WARN %s\n' "$*" >&2
}

fail() {
        local code="$1"
        shift
        printf 'ERROR[%s] %s\n' "$code" "$*" >&2
        exit "$code"
}

command_exists() {
        command -v "$1" >/dev/null 2>&1
}

require_commands() {
        local missing=()
        local cmd

        for cmd in "$@"
        do
                if ! command_exists "$cmd"
                then
                        missing+=("$cmd")
                fi
        done

        if [ "${#missing[@]}" -gt 0 ]
        then
                fail "$ERR_DEPENDENCY" "missing required command(s): ${missing[*]}"
        fi
}

require_file() {
        local path="$1"
        local label="$2"

        if [ ! -f "$path" ]
        then
                fail "$ERR_CONFIG" "$label not found: $path"
        fi
}

require_executable() {
        local path="$1"
        local label="$2"

        if [ ! -x "$path" ]
        then
                fail "$ERR_DEPENDENCY" "$label is not executable: $path"
        fi
}

require_value() {
        local name="$1"
        local value="${2:-}"

        if [ -z "$value" ]
        then
                fail "$ERR_USAGE" "$name is required"
        fi
}

run_or_fail() {
        local desc="$1"
        shift

        local output
        if ! output=$("$@" 2>&1)
        then
                fail "$ERR_RUNTIME" "$desc failed: $output"
        fi
}

try_run() {
        local desc="$1"
        shift

        local output
        if ! output=$("$@" 2>&1)
        then
                warn "$desc failed: $output"
                return 1
        fi
        return 0
}

ensure_parent_dir() {
        local path="$1"
        mkdir -p "$(dirname "$path")"
}

ensure_state_file() {
        ensure_parent_dir "$smb_state_file"
        if [ ! -f "$smb_state_file" ]
        then
                printf '{\n  "samba_security_type": "normal"\n}\n' > "$smb_state_file"
        fi
}

state_get_security_type() {
        ensure_state_file
        python3 - "$smb_state_file" <<'PY'
import json
import sys

path = sys.argv[1]
try:
    with open(path, "r", encoding="utf-8") as file:
        data = json.load(file)
except Exception:
    data = {}

value = str(data.get("samba_security_type") or "normal").strip().lower()
print(value if value in {"normal", "ads"} else "normal")
PY
}

state_set_security_type() {
        local security_type="$1"

        case "$security_type" in
                normal|ads) ;;
                *) fail "$ERR_USAGE" "invalid samba_security_type: $security_type" ;;
        esac

        ensure_state_file
        python3 - "$smb_state_file" "$security_type" <<'PY'
import json
import os
import sys

path = sys.argv[1]
security_type = sys.argv[2]

try:
    with open(path, "r", encoding="utf-8") as file:
        data = json.load(file)
        if not isinstance(data, dict):
            data = {}
except Exception:
    data = {}

data["samba_security_type"] = security_type
tmp = path + ".tmp"
with open(tmp, "w", encoding="utf-8") as file:
    json.dump(data, file, ensure_ascii=False, indent=2)
    file.write("\n")
os.replace(tmp, path)
PY
}

validate_security_type() {
        case "$1" in
                normal|ads) ;;
                *) fail "$ERR_USAGE" "sec_type must be one of normal, ads" ;;
        esac
}

validate_cache_policy() {
        case "$1" in
                true|false) ;;
                *) fail "$ERR_USAGE" "cache_policy must be one of true, false" ;;
        esac
}

reset_args() {
        sec_type=""
        username=""
        password=""
        cache="false"
        folder=""
        path=""
        fs_name=""
        volume_path=""
        realm=""
        dns=""
}

read_flag_value() {
        local flag="$1"
        local value="${2:-}"

        if [ -z "$value" ] || [[ "$value" == --* ]]
        then
                fail "$ERR_USAGE" "$flag requires a value"
        fi
        printf '%s' "$value"
}

parse_flags() {
        while [ "$#" -gt 0 ]
        do
                case "$1" in
                        --username)
                                username="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        --password)
                                password="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        --cache_policy)
                                cache="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        --folder)
                                folder="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        --path)
                                path="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        --fs_name)
                                fs_name="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        --volume_path)
                                volume_path="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        --realm)
                                realm="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        --dns)
                                dns="$(read_flag_value "$1" "${2:-}")"
                                shift 2
                                ;;
                        *)
                                fail "$ERR_USAGE" "unknown option: $1"
                                ;;
                esac
        done
}

get_fsid() {
        require_file "/etc/ceph/ceph.conf" "Ceph config"
        local fsid
        fsid=$(awk '$1 == "fsid" {print $3; exit}' /etc/ceph/ceph.conf)
        require_value "ceph fsid" "$fsid"
        printf '%s' "$fsid"
}

get_admin_key() {
        require_file "/etc/ceph/ceph.client.admin.keyring" "Ceph admin keyring"
        local admin_key
        admin_key=$(awk '$1 == "key" {print $3; exit}' /etc/ceph/ceph.client.admin.keyring)
        require_value "ceph admin key" "$admin_key"
        printf '%s' "$admin_key"
}

mount_is_active() {
        local target="$1"

        if command_exists findmnt
        then
                findmnt -rn --target "$target" >/dev/null 2>&1
                return $?
        fi

        if command_exists mountpoint
        then
                mountpoint -q "$target"
                return $?
        fi

        mount | awk -v target="$target" '$3 == target {found=1} END {exit found ? 0 : 1}'
}

ensure_fstab_entry() {
        local fs_name="$1"
        local volume_path="$2"
        local mount_path="$3"
        local fsid
        local admin_key
        local entry

        fsid="$(get_fsid)"
        admin_key="$(get_admin_key)"
        entry="admin@${fsid}.${fs_name}=${volume_path} ${mount_path} ceph name=admin,secret=${admin_key},rw,relatime,seclabel,defaults 0 0"

        if [ -f /etc/fstab ] && awk -v mount_path="$mount_path" '$2 == mount_path {found=1} END {exit found ? 0 : 1}' /etc/fstab
        then
                return 0
        fi

        printf '%s\n' "$entry" >> /etc/fstab
}

remove_fstab_path() {
        local mount_path="$1"
        local tmp

        if [ -z "$mount_path" ] || [ ! -f /etc/fstab ]
        then
                return 0
        fi

        tmp="$(mktemp)"
        awk -v mount_path="$mount_path" '$2 != mount_path {print}' /etc/fstab > "$tmp"
        cat "$tmp" > /etc/fstab
        rm -f "$tmp"
}

ensure_mount_path() {
        local mount_path="$1"

        if [ ! -d "$mount_path" ]
        then
                run_or_fail "create mount path" mkdir -p "$mount_path"
        fi
        run_or_fail "set mount path permission" chmod 0777 "$mount_path"
}

mount_ceph_volume() {
        local fs_name="$1"
        local volume_path="$2"
        local mount_path="$3"

        require_commands mount awk
        require_value "fs_name" "$fs_name"
        require_value "volume_path" "$volume_path"
        require_value "path" "$mount_path"

        ensure_mount_path "$mount_path"
        if ! mount_is_active "$mount_path"
        then
                run_or_fail "mount CephFS volume" mount -t ceph "admin@.${fs_name}=${volume_path}" "$mount_path"
        fi
        ensure_fstab_entry "$fs_name" "$volume_path" "$mount_path"
}

try_umount_path() {
        local mount_path="$1"

        if [ -z "$mount_path" ]
        then
                return 0
        fi

        if mount_is_active "$mount_path"
        then
                try_run "unmount $mount_path" umount -l -f "$mount_path" || true
        fi
}

write_normal_smb_conf() {
        ensure_parent_dir "$smb_conf"
        cat > "$smb_conf" <<'EOF'
[global]
workgroup = WORKGROUP
hosts allow = 0.0.0.0/0.0.0.0
security = user
passdb backend = tdbsam
usershare allow guests = yes
guest account = root
guest ok = yes
force user = root
log file = /var/log/samba/%m.log
log level = 10
EOF
}

write_ads_smb_conf() {
        local realm="$1"
        local workgroup="${realm%%.*}"
        workgroup="${workgroup^^}"

        ensure_parent_dir "$smb_conf"
        cat > "$smb_conf" <<EOF
[global]
workgroup = $workgroup
realm = $realm
hosts allow = 0.0.0.0/0.0.0.0
usershare allow guests = yes
usershare owner only = no
guest account = root
guest ok = yes
force user = root
security = ads
winbind separator = +
idmap config * : unix_nss_info = yes
vfs objects = acl_xattr
map acl inherit = Yes
store dos attributes = Yes
dedicated keytab file = /etc/krb5.keytab
server min protocol = SMB3
server max protocol = SMB3
log file = /var/log/samba/%m.log
log level = 10
EOF
}

append_share_config() {
        local share_name="$1"
        local share_path="$2"
        local cache_policy="$3"
        local fake_compression="${4:-false}"

        cat >> "$smb_conf" <<EOF

[$share_name]
comment = Share Directories
path = $share_path
writable = yes
public = yes
create mask = 0777
directory mask = 0777
EOF

        if [ "$fake_compression" = "true" ]
        then
                printf 'vfs objects = fake_compression\n' >> "$smb_conf"
        fi

        if [ "$cache_policy" = "true" ]
        then
                printf 'csc policy = programs\n' >> "$smb_conf"
        fi
}

samba_user_exists() {
        local smb_user="$1"

        if ! command_exists pdbedit
        then
                return 1
        fi
        pdbedit -L --debuglevel=1 2>/dev/null | awk -F ':' -v smb_user="$smb_user" '$1 == smb_user {found=1} END {exit found ? 0 : 1}'
}

list_managed_smb_users() {
        if ! command_exists pdbedit
        then
                return 0
        fi
        pdbedit -L --debuglevel=1 2>/dev/null | awk -F ':' '$1 != "root" && $1 != "ablecloud" && $1 != "" {print $1}'
}

ensure_system_user() {
        local smb_user="$1"

        if id "$smb_user" >/dev/null 2>&1
        then
                return 0
        fi
        run_or_fail "create system user $smb_user" useradd "$smb_user"
}

create_smb_user() {
        local smb_user="$1"
        local smb_password="$2"
        local output

        require_commands id useradd smbpasswd pdbedit
        if samba_user_exists "$smb_user"
        then
                fail "$ERR_RUNTIME" "SMB user already exists: $smb_user"
        fi

        ensure_system_user "$smb_user"
        if ! output=$(printf '%s\n%s\n' "$smb_password" "$smb_password" | smbpasswd -a -s "$smb_user" 2>&1)
        then
                fail "$ERR_RUNTIME" "create SMB user failed: $output"
        fi
}

update_smb_user() {
        local smb_user="$1"
        local smb_password="$2"
        local output

        require_commands smbpasswd pdbedit
        if ! samba_user_exists "$smb_user"
        then
                fail "$ERR_RUNTIME" "SMB user not found: $smb_user"
        fi

        if ! output=$(printf '%s\n%s\n' "$smb_password" "$smb_password" | smbpasswd -s "$smb_user" 2>&1)
        then
                fail "$ERR_RUNTIME" "update SMB user failed: $output"
        fi
}

delete_smb_user() {
        local smb_user="$1"

        require_commands smbpasswd
        if samba_user_exists "$smb_user"
        then
                try_run "delete SMB user $smb_user" smbpasswd -x "$smb_user" || true
        fi

        if id "$smb_user" >/dev/null 2>&1
        then
                try_run "delete system user $smb_user" userdel -r "$smb_user" || true
        fi
}

delete_all_managed_users() {
        local users=()
        local smb_user

        if ! command_exists pdbedit
        then
                return 0
        fi

        mapfile -t users < <(list_managed_smb_users)
        for smb_user in "${users[@]}"
        do
                delete_smb_user "$smb_user"
        done
}

add_firewall_samba() {
        if ! command_exists firewall-cmd
        then
                return 0
        fi

        try_run "add samba firewall service permanently" firewall-cmd --permanent --add-service=samba || true
        try_run "reload firewall" firewall-cmd --reload || true
}

remove_firewall_samba() {
        if ! command_exists firewall-cmd
        then
                return 0
        fi

        try_run "remove samba firewall service permanently" firewall-cmd --permanent --remove-service=samba || true
        try_run "reload firewall" firewall-cmd --reload || true
}

start_smb_services() {
        local security_type="$1"

        require_commands systemctl
        add_firewall_samba
        run_or_fail "enable smb service" systemctl enable smb.service
        run_or_fail "restart smb service" systemctl restart smb.service

        if [ "$security_type" = "ads" ]
        then
                run_or_fail "enable winbind service" systemctl enable winbind.service
                run_or_fail "restart winbind service" systemctl restart winbind.service
        fi
}

restart_smb_service() {
        require_commands systemctl
        if systemctl list-unit-files smb.service >/dev/null 2>&1
        then
                run_or_fail "restart smb service" systemctl restart smb.service
        fi
}

stop_smb_services() {
        if ! command_exists systemctl
        then
                return 0
        fi

        try_run "stop smb service" systemctl stop smb.service || true
        try_run "disable smb service" systemctl disable smb.service || true
        try_run "stop winbind service" systemctl stop winbind.service || true
        try_run "disable winbind service" systemctl disable winbind.service || true
}

apply_ads_dns() {
        local dns_server="$1"

        if [ ! -f "$network_ifcfg" ]
        then
                warn "network config not found, DNS update skipped: $network_ifcfg"
                return 0
        fi

        python3 - "$network_ifcfg" "$dns_server" <<'PY'
import sys

path = sys.argv[1]
dns = sys.argv[2]
with open(path, "r", encoding="utf-8") as file:
    lines = file.read().splitlines()

out = []
dns1_written = False
dns2_exists = any(line.startswith("DNS2=") for line in lines)

for line in lines:
    if line.startswith("DNS1="):
        if not dns2_exists:
            out.append("DNS2=" + line.split("=", 1)[1])
            dns2_exists = True
        if not dns1_written:
            out.append("DNS1=" + dns)
            dns1_written = True
        continue
    out.append(line)

if not dns1_written:
    out.append("DNS1=" + dns)

with open(path, "w", encoding="utf-8") as file:
    file.write("\n".join(out) + "\n")
PY

        if command_exists systemctl
        then
                try_run "restart NetworkManager" systemctl restart NetworkManager || true
        fi
}

restore_ads_dns() {
        if [ ! -f "$network_ifcfg" ]
        then
                return 0
        fi

        python3 - "$network_ifcfg" <<'PY'
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as file:
    lines = file.read().splitlines()

out = []
for line in lines:
    if line.startswith("DNS1="):
        continue
    if line.startswith("DNS2="):
        out.append("DNS1=" + line.split("=", 1)[1])
        continue
    out.append(line)

with open(path, "w", encoding="utf-8") as file:
    file.write("\n".join(out) + "\n")
PY

        if command_exists systemctl
        then
                try_run "restart NetworkManager" systemctl restart NetworkManager || true
        fi
}

join_ads_realm() {
        local ads_realm="$1"
        local join_user="$2"
        local join_password="$3"
        local output

        require_commands realm
        if command_exists update-crypto-policies
        then
                try_run "set crypto policy for AD support" update-crypto-policies --set DEFAULT:AD-SUPPORT || true
        fi

        if ! output=$(printf '%s\n' "$join_password" | realm join --membership-software=samba --client-software=winbind "$ads_realm" -U "$join_user" 2>&1)
        then
                fail "$ERR_RUNTIME" "realm join failed: $output"
        fi
}

helper_section_paths() {
        local path_json

        if ! path_json=$("$smb_conf_helper" -a arrayList 2>/dev/null)
        then
                return 0
        fi

        PATH_LIST_JSON="$path_json" python3 - <<'PY'
import json
import os

try:
    data = json.loads(os.environ.get("PATH_LIST_JSON", "{}"))
except Exception:
    data = {}

for value in data.values():
    path = value.get("path") if isinstance(value, dict) else None
    if path:
        print(path)
PY
}

json_array_from_lines() {
        python3 - <<'PY'
import json
import sys

values = [line.strip() for line in sys.stdin if line.strip()]
print(json.dumps(values, ensure_ascii=False))
PY
}

json_int_array_from_lines() {
        python3 - <<'PY'
import json
import sys

values = []
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        values.append(int(line))
    except ValueError:
        values.append(line)
print(json.dumps(values, ensure_ascii=False))
PY
}

list_smb_ports() {
        if command_exists ss
        then
                ss -ltnp 2>/dev/null | awk '/smb/ {n=split($4, parts, ":"); if (parts[n] != "") print parts[n]}' || true
                return 0
        fi

        if command_exists netstat
        then
                netstat -ltnp 2>/dev/null | awk '/smb/ && !/tcp6/ {n=split($4, parts, ":"); if (parts[n] != "") print parts[n]}' || true
                return 0
        fi
}

systemctl_value() {
        local service="$1"
        local property="$2"
        local value=""

        if command_exists systemctl
        then
                value=$(systemctl show --no-pager -p "$property" --value "$service" 2>/dev/null || true)
        fi
        printf '%s' "$value"
}

smb_conf_value() {
        local key="$1"

        if [ ! -f "$smb_conf" ]
        then
                return 0
        fi

        awk -F '=' -v key="$key" '
            {
                lhs = tolower($1)
                gsub(/^[ \t]+|[ \t]+$/, "", lhs)
                if (lhs == tolower(key)) {
                    rhs = $2
                    gsub(/^[ \t]+|[ \t]+$/, "", rhs)
                    print rhs
                    exit
                }
            }
        ' "$smb_conf"
}

handle_create() {
        reset_args
        sec_type="${1:-}"
        require_value "sec_type" "$sec_type"
        validate_security_type "$sec_type"
        shift || true
        parse_flags "$@"

        require_value "username" "$username"
        require_value "password" "$password"
        require_value "folder" "$folder"
        require_value "path" "$path"
        require_value "fs_name" "$fs_name"
        require_value "volume_path" "$volume_path"
        validate_cache_policy "$cache"
        require_executable "$smb_conf_helper" "smb_conf helper"

        mount_ceph_volume "$fs_name" "$volume_path" "$path"

        if [ "$sec_type" = "normal" ]
        then
                write_normal_smb_conf
                append_share_config "$folder" "$path" "$cache" "false"
                create_smb_user "$username" "$password"
                start_smb_services "normal"
                state_set_security_type "normal"
                return 0
        fi

        require_value "realm" "$realm"
        require_value "dns" "$dns"
        write_ads_smb_conf "$realm"
        append_share_config "$folder" "$path" "$cache" "true"
        apply_ads_dns "$dns"
        join_ads_realm "$realm" "$username" "$password"
        start_smb_services "ads"
        state_set_security_type "ads"
}

handle_share_folder_add() {
        reset_args
        parse_flags "$@"

        require_value "cache_policy" "$cache"
        require_value "folder" "$folder"
        require_value "path" "$path"
        require_value "fs_name" "$fs_name"
        require_value "volume_path" "$volume_path"
        validate_cache_policy "$cache"
        require_executable "$smb_conf_helper" "smb_conf helper"

        mount_ceph_volume "$fs_name" "$volume_path" "$path"

        if [ "$(state_get_security_type)" = "ads" ]
        then
                run_or_fail "add Samba share config" "$smb_conf_helper" -a confAdd -s "$folder" -p "$path" -c "$cache" -f true
        else
                run_or_fail "add Samba share config" "$smb_conf_helper" -a confAdd -s "$folder" -p "$path" -c "$cache"
        fi
        restart_smb_service
}

handle_share_folder_delete() {
        reset_args
        parse_flags "$@"

        require_value "folder" "$folder"
        require_value "path" "$path"
        require_executable "$smb_conf_helper" "smb_conf helper"

        try_umount_path "$path"
        remove_fstab_path "$path"
        run_or_fail "delete Samba share config" "$smb_conf_helper" -a confDelete -s "$folder"
        restart_smb_service
}

handle_user_create() {
        reset_args
        if [ "${1:-}" != "" ] && [[ "${1:-}" != --* ]]
        then
                sec_type="$1"
                shift
        fi
        parse_flags "$@"
        require_value "username" "$username"
        require_value "password" "$password"
        create_smb_user "$username" "$password"
}

handle_user_update() {
        reset_args
        if [ "${1:-}" != "" ] && [[ "${1:-}" != --* ]]
        then
                sec_type="$1"
                shift
        fi
        parse_flags "$@"
        require_value "username" "$username"
        require_value "password" "$password"
        update_smb_user "$username" "$password"
}

handle_user_delete() {
        reset_args
        if [ "${1:-}" != "" ] && [[ "${1:-}" != --* ]]
        then
                sec_type="$1"
                shift
        fi
        parse_flags "$@"
        require_value "username" "$username"
        delete_smb_user "$username"
}

handle_delete() {
        require_executable "$smb_conf_helper" "smb_conf helper"

        local share_paths=()
        local share_path
        mapfile -t share_paths < <(helper_section_paths)

        for share_path in "${share_paths[@]}"
        do
                try_umount_path "$share_path"
                remove_fstab_path "$share_path"
        done

        write_normal_smb_conf
        stop_smb_services
        remove_firewall_samba

        if [ "$(state_get_security_type)" = "ads" ]
        then
                restore_ads_dns
        fi

        delete_all_managed_users
        state_set_security_type "normal"
}

handle_select() {
        require_executable "$smb_conf_helper" "smb_conf helper"

        local hostname_value
        local ip_address
        local path_list_json
        local ports_json
        local users_json
        local security_type
        local smb_names
        local smb_status
        local smb_state
        local winbind_names
        local winbind_status
        local winbind_state
        local realm_value

        hostname_value=$(hostname | cut -d '.' -f1)
        ip_address=$(awk -v host="${hostname_value}-mngt" '$2 == host {print $1; exit}' /etc/hosts 2>/dev/null || true)
        path_list_json=$("$smb_conf_helper" -a arrayList)
        ports_json=$(list_smb_ports | sort -u | json_int_array_from_lines)
        users_json=$(list_managed_smb_users | json_array_from_lines)
        security_type="$(state_get_security_type)"
        smb_names="$(systemctl_value smb.service Names)"
        smb_status="$(systemctl_value smb.service ActiveState)"
        smb_state="$(systemctl_value smb.service UnitFileState)"

        if [ "$security_type" = "ads" ]
        then
                winbind_names="$(systemctl_value winbind.service Names)"
                winbind_status="$(systemctl_value winbind.service ActiveState)"
                winbind_state="$(systemctl_value winbind.service UnitFileState)"
                realm_value="$(smb_conf_value realm)"

                PATH_LIST_JSON="$path_list_json" \
                PORTS_JSON="$ports_json" \
                USERS_JSON="$users_json" \
                HOSTNAME_VALUE="$hostname_value" \
                IP_ADDRESS="$ip_address" \
                SECURITY_TYPE="$security_type" \
                SMB_NAMES="$smb_names" \
                SMB_STATUS="$smb_status" \
                SMB_STATE="$smb_state" \
                WINBIND_NAMES="$winbind_names" \
                WINBIND_STATUS="$winbind_status" \
                WINBIND_STATE="$winbind_state" \
                REALM_VALUE="$realm_value" \
                python3 - <<'PY'
import json
import os

def load_json(name, default):
    try:
        return json.loads(os.environ.get(name, ""))
    except Exception:
        return default

result = {
    "names": [os.environ.get("SMB_NAMES", ""), os.environ.get("WINBIND_NAMES", "")],
    "status": [os.environ.get("SMB_STATUS", ""), os.environ.get("WINBIND_STATUS", "")],
    "state": [os.environ.get("SMB_STATE", ""), os.environ.get("WINBIND_STATE", "")],
    "hostname": os.environ.get("HOSTNAME_VALUE", ""),
    "security_type": os.environ.get("SECURITY_TYPE", "ads"),
    "ip_address": os.environ.get("IP_ADDRESS", ""),
    "port": load_json("PORTS_JSON", []),
    "realm": os.environ.get("REALM_VALUE", ""),
    "users": load_json("USERS_JSON", []),
    "path_list": load_json("PATH_LIST_JSON", {}),
}

print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
PY
                return 0
        fi

        PATH_LIST_JSON="$path_list_json" \
        PORTS_JSON="$ports_json" \
        USERS_JSON="$users_json" \
        HOSTNAME_VALUE="$hostname_value" \
        IP_ADDRESS="$ip_address" \
        SECURITY_TYPE="$security_type" \
        SMB_NAMES="$smb_names" \
        SMB_STATUS="$smb_status" \
        SMB_STATE="$smb_state" \
        python3 - <<'PY'
import json
import os

def load_json(name, default):
    try:
        return json.loads(os.environ.get(name, ""))
    except Exception:
        return default

result = {
    "names": os.environ.get("SMB_NAMES", ""),
    "status": os.environ.get("SMB_STATUS", ""),
    "state": os.environ.get("SMB_STATE", ""),
    "hostname": os.environ.get("HOSTNAME_VALUE", ""),
    "ip_address": os.environ.get("IP_ADDRESS", ""),
    "security_type": os.environ.get("SECURITY_TYPE", "normal"),
    "port": load_json("PORTS_JSON", []),
    "users": load_json("USERS_JSON", []),
    "path_list": load_json("PATH_LIST_JSON", {}),
}

print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
PY
}

main() {
        require_commands python3 awk

        local action="${1:-}"
        require_value "action" "$action"
        shift || true

        case "$action" in
                create)
                        handle_create "$@"
                        ;;
                share_folder_add)
                        handle_share_folder_add "$@"
                        ;;
                share_folder_delete)
                        handle_share_folder_delete "$@"
                        ;;
                user_create)
                        handle_user_create "$@"
                        ;;
                user_update)
                        handle_user_update "$@"
                        ;;
                user_delete)
                        handle_user_delete "$@"
                        ;;
                delete)
                        handle_delete "$@"
                        ;;
                select)
                        handle_select "$@"
                        ;;
                *)
                        fail "$ERR_USAGE" "unknown action: $action"
                        ;;
        esac
}

main "$@"
