#!/bin/sh
#
# Copyright 2015-2021 the Pacemaker project contributors
#
# The version control history for this file may have further details.
#
# This source code is licensed under the GNU General Public License version 2
# or later (GPLv2+) WITHOUT ANY WARRANTY.
#
##############################################################################
# Alert script policy
# ===================
#
# 이 스크립트는 Pacemaker alert agent 예제 스크립트를 기반으로 하며,
# 기본 alert 로그와 별도로 장애 분석용 진단 로그를 남깁니다.
#
# 진단 로그는 시스템 재부팅/셧다운 뒤 사라질 수 있는 dmesg 내용과
# corosync journal 내용을 같은 파일에 누적해 fencing, node, resource
# 이벤트 당시의 원인을 추적할 수 있게 합니다.
##############################################################################

# Explicitly list all environment variables used, to make static analysis happy
: ${CRM_alert_version:=""}
: ${CRM_alert_recipient:=""}
: ${CRM_alert_node_sequence:=""}
: ${CRM_alert_timestamp:=""}
: ${CRM_alert_kind:=""}
: ${CRM_alert_node:=""}
: ${CRM_alert_desc:=""}
: ${CRM_alert_task:=""}
: ${CRM_alert_rsc:=""}
: ${CRM_alert_attribute_name:=""}
: ${CRM_alert_attribute_value:=""}
: ${CRM_alert_interval:=""}
: ${CRM_alert_target_rc:=""}

# No one will probably ever see this echo, unless they run the script manually.
if [ -z "$CRM_alert_version" ]; then
    echo "$0 must be run by Pacemaker version 1.1.15 or later"
    exit 0
fi

# Alert agents must always handle the case where no recipients are defined.
if [ -z "${CRM_alert_recipient}" ]; then
    echo "$0 requires a recipient configured with a full filename path"
    exit 0
fi

debug_exec_order_default="false"

# Pacemaker passes instance attributes to alert agents as environment variables.
: ${debug_exec_order=${debug_exec_order_default}}

diagnostic_log_default="/var/log/pcmk_alert_detail.log"
: ${CRM_alert_diagnostic_log:=${diagnostic_log_default}}
: ${diagnostic_dmesg_lines:=300}
: ${diagnostic_corosync_lines:=300}

if [ "${debug_exec_order}" = "true" ]; then
    tstamp=$(printf "%04d. " "$CRM_alert_node_sequence")
    if [ -n "$CRM_alert_timestamp" ]; then
        tstamp="${tstamp} $CRM_alert_timestamp ($(date "+%H:%M:%S.%06N")): "
    fi
else
    if [ -n "$CRM_alert_timestamp" ]; then
        tstamp="$(date "+%F %H:%M:%S"): "
    else
        tstamp="$(date "+%F %H:%M:%S"): "
    fi
fi

##############################################################################
# Utility functions
##############################################################################

command_exists() {
    command -v "$1" > /dev/null 2>&1
}

capture_diagnostics() {
    reason="$1"

    if ! {
        echo "##############################################################################"
        echo "$(date '+%F %T'): alert diagnostics reason='${reason}' kind='${CRM_alert_kind}' node='${CRM_alert_node}' desc='${CRM_alert_desc}' task='${CRM_alert_task}' rsc='${CRM_alert_rsc}'"
        echo "------------------------------------------------------------------------------"
        echo "[dmesg]"
        if command_exists dmesg; then
            dmesg -T 2>&1 | tail -n "${diagnostic_dmesg_lines}"
        else
            echo "dmesg command not found"
        fi
        echo "------------------------------------------------------------------------------"
        echo "[corosync]"
        if command_exists journalctl; then
            journalctl -u corosync -u corosync.service -b --no-pager -n "${diagnostic_corosync_lines}" 2>&1
        elif [ -r /var/log/cluster/corosync.log ]; then
            tail -n "${diagnostic_corosync_lines}" /var/log/cluster/corosync.log 2>&1
        elif [ -r /var/log/corosync/corosync.log ]; then
            tail -n "${diagnostic_corosync_lines}" /var/log/corosync/corosync.log 2>&1
        else
            echo "corosync log source not found"
        fi
        echo
    } >> "${CRM_alert_diagnostic_log}" 2>&1
    then
        echo "${tstamp}Failed to write diagnostic log '${CRM_alert_diagnostic_log}'" >> "${CRM_alert_recipient}" 2>/dev/null
    fi
}

##############################################################################
# Main alert handling
##############################################################################

case $CRM_alert_kind in
    node)
        echo "${tstamp}Node '${CRM_alert_node}' is now '${CRM_alert_desc}'" >> "${CRM_alert_recipient}"
        capture_diagnostics "node-${CRM_alert_desc}"
        ;;
    fencing)
        # fencing alert 전체를 다 처리하지 않고,
        # 실제로 상대 노드 상실(lost) 상황일 때만 처리합니다.
        if [ "$CRM_alert_desc" != "lost" ]; then
            echo "${tstamp}Ignoring fencing alert because desc='${CRM_alert_desc}'" >> "${CRM_alert_recipient}"
            exit 0
        fi

        sleep 7

        echo "${tstamp}Fencing ${CRM_alert_desc}" >> "${CRM_alert_recipient}"
        capture_diagnostics "fencing-${CRM_alert_desc}"
        ;;
    resource)
        if [ "${CRM_alert_interval}" = "0" ]; then
            CRM_alert_interval=""
        else
            CRM_alert_interval=" (${CRM_alert_interval})"
        fi

        if [ "${CRM_alert_target_rc}" = "0" ]; then
            CRM_alert_target_rc=""
        else
            CRM_alert_target_rc=" (target: ${CRM_alert_target_rc})"
        fi

        case ${CRM_alert_desc} in
            Cancelled)
                ;;
            *)
                echo "${tstamp}Resource operation '${CRM_alert_task}${CRM_alert_interval}' for '${CRM_alert_rsc}' on '${CRM_alert_node}': ${CRM_alert_desc}${CRM_alert_target_rc}" >> "${CRM_alert_recipient}"
                capture_diagnostics "resource-${CRM_alert_rsc}-${CRM_alert_task}-${CRM_alert_desc}"
                ;;
        esac
        ;;
    attribute)
        echo "${tstamp}Attribute '${CRM_alert_attribute_name}' on node '${CRM_alert_node}' was updated to '${CRM_alert_attribute_value}'" >> "${CRM_alert_recipient}"
        ;;
    *)
        echo "${tstamp}Unhandled $CRM_alert_kind alert" >> "${CRM_alert_recipient}"
        env | grep CRM_alert >> "${CRM_alert_recipient}"
        capture_diagnostics "unhandled-${CRM_alert_kind}"
        ;;
esac
