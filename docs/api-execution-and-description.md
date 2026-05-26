# ABLESTACK API 실행 및 상세 설명

작성일: 2026-05-22

이 문서는 현재 Go 코드의 실제 route, RPM 패키징 구성, `/etc/ablestack` 설정 경로 정책을 기준으로 정리했다. Swagger 예시는 `/swagger/index.html`에서 확인할 수 있고, 이 문서는 운영자가 설치, 설정, API 호출 흐름을 한 번에 파악하는 것을 목표로 한다.

## 기본 정보

| 항목 | 값 |
| --- | --- |
| 기본 URL | `http://<ablecube-ip>:8090/api/v1` |
| Swagger | `http://<ablecube-ip>:8090/swagger/index.html` |
| API 서버 진입점 | `cmd/apiserver/main.go` |
| 주요 handler | `internal/handler` |
| 주요 model | `internal/model` |
| 클러스터 설정 파일 | `/etc/ablestack/properties/cluster.json` |
| 서비스 환경 파일 | `/etc/ablestack/ablestack-api.env` |
| VM 설정 생성물 | `/etc/ablestack/vmconfig` |
| XML 템플릿 | `/etc/ablestack/xml-template` |

주의: 현재 `main.go`의 listen port는 `8090`으로 고정되어 있다. `ABLESTACK_API_PORT`는 서버 listen port가 아니라 노드 간 원격 API URL을 만들 때 사용된다. RPM 설치 시 `firewall-cmd`가 있으면 `8090/tcp`를 runtime/permanent 모두 열고, `firewalld`가 꺼져 있으면 `enable --now`를 시도한다.

## RPM 설치 및 경로 정책

RPM은 `cmd/apiserver/main.go`를 `/usr/bin/ablestack-api`로 빌드하고 systemd 서비스로 설치한다. 설치 후 `%post` 스크립트에서 설정 병합, 방화벽 포트 오픈, 서비스 enable/start를 수행한다.

### 빌드

```bash
VERSION=0.1.4 RELEASE=1 ./scripts/build-rpm.sh
```

생성 위치:

```text
dist/rpm/rpmbuild/RPMS/<arch>/ablestack-api-<version>-<release>.el9.<arch>.rpm
dist/rpm/rpmbuild/SRPMS/ablestack-api-<version>-<release>.el9.src.rpm
```

### 설치/업데이트

로컬 RPM은 `rpm -Uvh`보다 `dnf install` 또는 `dnf upgrade`로 설치한다. `dnf`를 사용해야 저장소에 있는 필수 패키지를 함께 해결할 수 있다.

```bash
dnf install dist/rpm/rpmbuild/RPMS/x86_64/ablestack-api-0.1.4-1.el9.x86_64.rpm
```

업데이트:

```bash
dnf upgrade dist/rpm/rpmbuild/RPMS/x86_64/ablestack-api-0.1.4-1.el9.x86_64.rpm
```

### RPM 설치 시 자동 처리

| 처리 | 설명 |
| --- | --- |
| Binary 설치 | `/usr/bin/ablestack-api` |
| systemd unit 설치 | `/usr/lib/systemd/system/ablestack-api.service` |
| 서비스 환경 파일 설치 | `/etc/ablestack/ablestack-api.env` |
| 설정 파일 설치 | `/etc/ablestack/config.json`, `/etc/ablestack/properties/cluster.json` |
| 템플릿 설치 | `/etc/ablestack/xml-template/*` |
| shell 리소스 설치 | `/etc/ablestack/shell/*` |
| VM 설정 디렉터리 생성 | `/etc/ablestack/vmconfig/ccvm`, `/etc/ablestack/vmconfig/scvm` |
| 설정 병합 | 기존 JSON 값은 유지하고 누락된 key만 추가 |
| 방화벽 처리 | `firewall-cmd`가 있으면 `firewalld enable --now`, `8090/tcp` open |
| API 서비스 처리 | `systemctl enable --now ablestack-api.service`, 업데이트 시 `try-restart` |

### RPM dependency 기준

설치 자체를 막는 hard dependency는 최소화한다.

| 구분 | 패키지/명령 | 설명 |
| --- | --- | --- |
| Hard dependency | `systemd`, `python3` | 서비스 등록과 JSON 병합에 필요 |
| Build dependency | `golang`, `libvirt-devel`, `pkgconfig` | RPM 빌드 서버에서만 필요 |
| Recommended | `firewalld` | 있으면 설치 시 8090/tcp 오픈 |
| Runtime command | `pcs`, `virsh`, `qemu-img`, `genisoimage`, `ssh/scp`, `ceph`, `rbd`, `lsblk`, `nmcli` | 해당 기능 실행 시 필요. RPM 설치를 막지는 않음 |

예를 들어 `pcs`가 없는 저장소에서도 RPM 설치는 가능해야 한다. 단, PCS 관련 API를 호출하면 실제 명령이 없으므로 해당 API에서 실패한다.

### 업데이트 시 설정 보존

`/etc/ablestack` 아래 설정 파일은 RPM spec에서 `%config(noreplace)`로 관리한다. 따라서 업데이트 시 기존 운영 파일을 덮어쓰지 않는다.

`config.json`과 `properties/cluster.json`은 새 RPM에서 `.rpmnew`가 생기면 `merge-json-defaults.py`가 기존 파일에 없는 key만 추가한다. 기존 값은 유지된다. 기존 `cluster.json`이 0바이트라면 운영 데이터가 없다고 보고 기본 스켈레톤을 채운다.

초기 `cluster.json`은 값 없이 구조만 제공한다.

```json
{
  "clusterConfig": {
    "type": "",
    "backup_path": "",
    "ccvm": {
      "ip": ""
    },
    "mngtNic": {
      "cidr": "",
      "gw": "",
      "dns": ""
    },
    "pcsCluster": {
      "hostname1": "",
      "hostname2": "",
      "hostname3": ""
    },
    "hosts": [
      {
        "index": "",
        "hostname": "",
        "ablecube": "",
        "scvmMngt": "",
        "ablecubePn": "",
        "scvm": "",
        "scvmCn": ""
      }
    ],
    "external_timeserver": "",
    "iscsi_storage": ""
  },
  "systemProfile": {
    "bootstrap": {
      "scvm": "",
      "ccvm": "",
      "wall": "",
      "gfs_configure": "",
      "local_configure": ""
    },
    "license": {
      "status": "",
      "type": ""
    },
    "security_patch": {
      "status": ""
    }
  }
}
```

### 환경 변수

| 변수 | 기본값 | 설명 |
| --- | --- | --- |
| `CUBE_CONFIG_PATH` | `/etc/ablestack/config.json` | neighbor 설정 파일 |
| `ABLESTACK_CLUSTER_JSON` | `/etc/ablestack/properties/cluster.json` | cluster.json 절대 경로 override |
| `ABLESTACK_CONFIG_PATH` | `/etc/ablestack` | properties, xml-template, shell 기준 루트 |
| `ABLESTACK_STATE_PATH` | `/etc/ablestack/vmconfig` | 생성된 CCVM/SCVM XML, secret 등 VM 설정 루트 |
| `ABLESTACK_API_SCHEME` | `http` | 노드 간 API 호출 scheme |
| `ABLESTACK_API_PORT` | `8090` | 노드 간 API 호출 대상 port |
| `ABLESTACK_SECURITY_PATCH_SCRIPT` | `/usr/local/sbin/security_patch.sh` | 보안 패치 스크립트 override |

### 서비스 확인

```bash
systemctl status ablestack-api.service
journalctl -u ablestack-api.service -f
curl -sS http://127.0.0.1:8090/api/v1/cube/cluster/health
```

## 전체 구조

```text
Client or Web UI
  -> Gin Router: cmd/apiserver/main.go
    -> Handler: internal/handler/<domain>
      -> Model/DTO: internal/model/<domain>
      -> Service: internal/service/clusterconfig, internal/service/controller
      -> OS command: ceph, pcs, virsh, nmcli, lsblk, rbd, mysqldump, ssh-keyscan
```

### 주요 패키지 역할

| 경로 | 역할 |
| --- | --- |
| `cmd/apiserver` | Gin 서버 생성, middleware, route 등록, Swagger 노출 |
| `internal/handler/cube` | Cube 운영 API 대부분 구현 |
| `internal/handler/glue` | Glue 상태/auth 조회 API |
| `internal/handler/mold` | Mold/CCVM 관련 상태 API |
| `internal/handler/pcs` | PCS 상태 조회 API |
| `internal/model/cube` | Cube API 요청/응답 구조체 |
| `internal/service/clusterconfig` | `cluster.json`, `/etc/hosts` 적용 로직 |
| `internal/service/controller` | neighbor 관리, 주기 실행 handler 등록 |
| `docs` | Swagger 산출물 및 운영 문서 |

## 공통 사용법

### Health check

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/cluster/health
```

응답 예:

```json
{
  "status": "ok"
}
```

### JSON POST 기본 형식

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/<path> \
  -H "Content-Type: application/json" \
  -d '{"action":"status"}'
```

## API 요약

| 종류 | Method | Endpoint | Action/Query | 설명 |
| --- | --- | --- | --- | --- |
| Auth | POST | `/auth/login` | `id,password` | 웹/API용 Bearer access token 발급 |
| Auth | GET | `/auth/me` | Bearer token | 현재 access token 검증 |
| Auth | POST | `/auth/internal-token/rotate` | Bearer token | 내부 호스트 간 공유 토큰 교체 |
| 디스크 | GET | `/cube/disk` | `action=list,gfs,rbd,detail`, `view=tree,flat,list` | 디스크, multipath, RBD, RAID controller 정보 조회 |
| 네트워크 | GET | `/cube/nics` | `action=list,detail` | ethernet, bridge, bond, IP, MAC, speed 조회 |
| 호스트 | GET | `/cube/hosts` | 없음 | `/etc/hosts`를 네트워크/역할별로 조회 |
| 클러스터 | GET | `/cube/cluster/health` | `option=host,scvm,ccvm` | API 생존 및 대상 노드 상태 점검 |
| 클러스터 | GET | `/cube/cluster/config` | 없음 | `cluster.json`의 `clusterConfig` 조회 |
| 클러스터 | POST | `/cube/cluster/apply` | `insert,remove,reset,check` | 클러스터 구성 오케스트레이션 |
| 클러스터 | POST | `/cube/cluster/apply-local` | 내부용 | 각 노드에서 실제 cluster config 적용 |
| System Profile | GET | `/cube/system/config` | 없음 | `systemProfile` 조회 |
| System Profile | POST | `/cube/system/config` | `status,update,allUpdate,reset` | `systemProfile` 조회/수정/초기화 |
| URL | GET | `/cube/url` | `option=cloudCenter,wallCenter,storageCenter` | Cloud/Wall/Storage 접속 URL 반환 |
| CloudInit | POST | `/cube/cloudinit/generate` | body | 수동 파라미터 기반 CCVM/SCVM cloud-init ISO 생성 |
| CloudInit | POST | `/cube/cloudinit/ccvm/generate` | body optional | `cluster.json` 기반 CCVM cloud-init ISO 생성 및 ablecubePn 대상 복사 |
| CloudInit | POST | `/cube/cloudinit/scvm/generate` | 없음 | 현재 노드와 `cluster.json` 기반 SCVM cloud-init ISO 생성 |
| CCVM | GET | `/cube/ccvm/status` | 없음 | CCVM 상태 조회 |
| CCVM | POST | `/cube/ccvm/xml` | body | CCVM libvirt XML 생성 및 대상 노드 배포 |
| CCVM | POST | `/cube/ccvm/lifecycle` | `setup,reset,copy,start,stop,restart,delete` | Cloud Center VM lifecycle |
| CCVM | POST | `/cube/ccvm/edit` | body | CCVM CPU/메모리 XML 수정 |
| CCVM | POST | `/cube/ccvm/secondary/resize` | body | CCVM secondary 용량 추가 |
| CCVM | POST | `/cube/ccvm/service/control` | `start,restart,stop,status` | CCVM의 서비스 제어 |
| CCVM Snapshot | POST | `/cube/ccvm/snap` | `list,backup,rollback` | HCI/HCI-FS CCVM RBD snapshot |
| CCVM Backup | POST | `/cube/ccvm/backup` | `backup,status,list,overview,schedule,unschedule,schedule-delete,unschedule-delete` | VM/Standalone CCVM 파일 백업 |
| CCVM Restore | POST | `/cube/ccvm/restore` | body | CCVM 파일 백업 복구 |
| SCVM | GET | `/cube/scvm/status` | 없음 | Storage Center VM 상태 조회 |
| SCVM | POST | `/cube/scvm/xml` | body | SCVM libvirt XML 및 hugepage 설정 생성 |
| SCVM | POST | `/cube/scvm/lifecycle` | `setup,reset,start,stop,delete,resource` | Storage Center VM lifecycle |
| PCS | POST | `/cube/pcs/control` | `setup,config,create,enable,disable,move,cleanup,status,remove,destroy,stop,sync,ccvm-status` | Cloud Center PCS 제어 |
| Glue | GET | `/cube/gluecluster/status` | 없음 | Glue cluster 상세 상태 조회 |
| Glue | POST | `/cube/gluecluster/update` | `set_noout,unset_noout` | 유지보수 모드 설정/해제 |
| GFS | GET | `/cube/gfs/resource/status` | 없음 | GFS 관련 PCS 리소스 상태 |
| GFS | GET | `/cube/gfs/disk/status` | 없음 | GFS2 마운트 디스크 상태 |
| License | POST | `/cube/license` | `status,register` | 라이선스 조회/등록 |
| Version | POST | `/cube/version/update` | `info,run` | 마운트된 ISO 버전 조회/업데이트 |
| Security | POST | `/cube/security/patch` | body flags | 보안 패치 실행 및 상태 업데이트 |
| DB | POST | `/cube/db/dump` | `instantBackup,regularBackup,deleteOldBackup,checkBackup,deactiveBackup` | CCVM DB dump 및 스케줄 관리 |

## Auth API

웹 UI/API 클라이언트는 `/auth/login`에서 발급받은 access token을 `Authorization: Bearer <token>` 형태로 사용한다.
인증은 Linux 계정 인증만 사용하며, `/etc/ablestack/auth.json`의 `linux.allowed_users`, `linux.allowed_groups`로 허용 계정을 제한한다.
기본값은 `root` 사용자 또는 `wheel` 그룹 사용자를 허용한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"id":"root","password":"<linux-password>"}'
```

```bash
curl http://<ablecube-ip>:8090/api/v1/auth/me \
  -H "Authorization: Bearer <access_token>"
```

라이선스 등록이 성공하면 `cluster.json`의 `security.internal_token`이 없을 때 자동으로 생성된다.
내부 fan-out 요청은 `X-Cube-Internal-Token` 헤더를 사용하며, 토큰 교체는 아래 API로 단순화한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/auth/internal-token/rotate \
  -H "Authorization: Bearer <access_token>"
```

## Cluster API

### `GET /cube/cluster/health`

기본 호출은 API 서버 생존 확인이다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/cluster/health
```

대상 노드 점검:

```bash
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/cluster/health?option=host"
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/cluster/health?option=scvm"
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/cluster/health?option=ccvm"
```

특정 hostname만 점검:

```bash
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/cluster/health?option=host&target_hostname=ablecube31-1,ablecube31-2"
```

### `GET /cube/cluster/config`

`cluster.json`의 `clusterConfig` 섹션을 반환한다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/cluster/config
```

### `cluster.json` 구조

현재 `clusterConfig.ccvm`에는 CCVM IP만 저장한다. 관리망 CIDR, gateway, DNS는 `mngtNic`로 분리한다.

```json
{
  "clusterConfig": {
    "type": "ablestack-vm",
    "backup_path": "/mnt/glue-gfs/backup/ccvm",
    "ccvm": {
      "ip": "10.10.31.10"
    },
    "mngtNic": {
      "cidr": "16",
      "gw": "10.10.0.1",
      "dns": "8.8.8.8"
    },
    "pcsCluster": {
      "hostname1": "10.10.31.1",
      "hostname2": "10.10.31.2",
      "hostname3": ""
    },
    "hosts": [
      {
        "index": "1",
        "hostname": "ablecube31-1",
        "ablecube": "10.10.31.1"
      }
    ],
    "external_timeserver": "time.google.com",
    "iscsi_storage": "false"
  }
}
```

HCI/HCI-filesystem 계열은 SCVM 관련 IP가 필요하므로 `hosts`에 아래 필드가 추가된다.

| 필드 | 의미 |
| --- | --- |
| `index` | 노드 순번. SCVM hostname 생성 시 `scvm1`, `scvm2`처럼 사용 |
| `hostname` | ablecube hostname |
| `ablecube` | 관리망 ablecube IP |
| `scvmMngt` | SCVM 관리망 IP |
| `ablecubePn` | ablecube public/storage network IP. CCVM XML/cloud-init fan-out 대상 |
| `scvm` | SCVM public/storage network IP |
| `scvmCn` | SCVM cluster network IP |

`ablestack-vm`에서는 빈 SCVM 필드를 응답에 내보내지 않는다.

### `POST /cube/cluster/apply`

오케스트레이터 API다. 요청을 받은 노드가 대상 host를 계산하고 각 노드의 `/cube/cluster/apply-local`을 호출한다.

지원 action:

| Action | 설명 | 주요 필수값 |
| --- | --- | --- |
| `insert` | `cluster.json` 반영 및 `/etc/hosts` 재구성 | `type`, `ccvm`, `hosts`, `pcs_cluster_list`, `iscsi_storage` |
| `remove` | 지정 hostname 제거 | `target_hostname` 또는 `remove_hostname` |
| `reset` | `cluster.json`을 기본값으로 초기화 | 없음 |
| `check` | 대상 ablecube API health 확인 | `hosts`, `type`, `ccvm` 또는 기존 `cluster.json` |

`insert` 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/cluster/apply \
  -H "Content-Type: application/json" \
  -d '{
    "action": "insert",
    "option": "add",
    "type": "ablestack-hci",
    "ccvm": {
      "ip": "10.10.12.10"
    },
    "mngtNic": {
      "cidr": "16",
      "gw": "10.10.0.1",
      "dns": "8.8.8.8"
    },
    "external_timeserver": "time.google.com",
    "iscsi_storage": "false",
    "pcs_cluster_list": ["10.10.12.1", "10.10.12.2", "10.10.12.3"],
    "hosts": [
      {
        "index": "1",
        "hostname": "ablecube12-1",
        "ablecube": "10.10.12.1",
        "scvmMngt": "10.10.12.11",
        "ablecubePn": "100.100.12.1",
        "scvm": "100.100.12.11",
        "scvmCn": "100.200.12.11"
      }
    ]
  }'
```

`remove` 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/cluster/apply \
  -H "Content-Type: application/json" \
  -d '{"action":"remove","target_hostname":"ablecube12-3"}'
```

`reset` 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/cluster/apply \
  -H "Content-Type: application/json" \
  -d '{"action":"reset"}'
```

`check` 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/cluster/apply \
  -H "Content-Type: application/json" \
  -d '{"action":"check","option":"all"}'
```

### `POST /cube/cluster/apply-local`

내부 실행 API다. `/cube/cluster/apply`가 노드별로 호출하는 실제 작업 API이며 직접 호출은 권장하지 않는다.

## System Profile API

### `GET /cube/system/config`

`cluster.json`의 `systemProfile`을 반환한다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/system/config
```

### `POST /cube/system/config`

지원 action:

| Action | 설명 |
| --- | --- |
| `status` | 전체 또는 특정 `depth1/depth2` 조회 |
| `update` | 특정 `depth1/depth2` 값 수정 |
| `allUpdate` | 클러스터 타입에 맞는 bootstrap 완료 상태 일괄 반영 |
| `reset` | systemProfile 초기화 |

조회:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/system/config \
  -H "Content-Type: application/json" \
  -d '{"action":"status","depth1":"bootstrap","depth2":"scvm"}'
```

전체 fan-out 수정:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/system/config \
  -H "Content-Type: application/json" \
  -d '{"action":"update","depth1":"bootstrap","depth2":"wall","value":"true","option":"all"}'
```

## Host, Disk, NIC API

### `GET /cube/hosts`

`/etc/hosts`를 읽고 localhost, management/public/client network, role 기준으로 정리해 반환한다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/hosts
```

### `GET /cube/disk`

지원 query:

| Query | 설명 |
| --- | --- |
| `action=list` | 일반 디스크 목록. RAID controller 포함 |
| `action=gfs` | GFS용 multipath/single path 목록. RAID controller 제외 |
| `action=rbd` | RBD map 디스크 목록 |
| `action=detail` | GFS와 유사한 상세 분류 |
| `view=tree` | 기본 tree 응답 |
| `view=flat` | 평탄화된 장치 목록 |
| `view=list` | list view |

```bash
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/disk?action=list"
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/disk?action=gfs"
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/disk?action=detail&view=flat"
```

### `GET /cube/nics`

지원 query:

| Query | 설명 |
| --- | --- |
| `action=list` | ethernet, bridge, bond 등 기본 목록 |
| `action=detail` | MAC, IPv4, IPv6, members, speed, bond option 등 상세 목록 |

```bash
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/nics?action=list"
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/nics?action=detail"
```

## URL API

### `GET /cube/url`

Cloud Center, Wall Center, Storage Center 접속 URL을 반환한다.

```bash
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/url?option=cloudCenter"
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/url?option=wallCenter"
curl -sS "http://<ablecube-ip>:8090/api/v1/cube/url?option=storageCenter"
```

## CloudInit API

cloud-init API는 CCVM/SCVM 부팅에 필요한 ISO를 생성한다. 공통으로 `/etc/hosts`, `/root/.ssh/id_rsa`, `/root/.ssh/id_rsa.pub`를 읽어 ISO에 포함한다.

### `POST /cube/cloudinit/generate`

모든 값을 직접 지정하는 저수준 API다. 일반 운영 화면에서는 아래의 CCVM/SCVM 전용 API를 사용하는 것이 더 안전하다.

SCVM 수동 생성 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/cloudinit/generate \
  -H "Content-Type: application/json" \
  -d '{
    "type": "scvm",
    "iso_path": "/var/lib/libvirt/images/scvm-cloudinit.iso",
    "hostname": "scvm1",
    "hosts": "/etc/hosts",
    "privkey": "/root/.ssh/id_rsa",
    "pubkey": "/root/.ssh/id_rsa.pub",
    "mgmt_nic": "enp0s20",
    "mgmt_ip": "10.10.31.11",
    "mgmt_prefix": 16,
    "mgmt_gw": "10.10.0.1",
    "dns": "8.8.8.8",
    "pn_nic": "enp0s21",
    "pn_ip": "100.100.31.11",
    "pn_prefix": 16,
    "cn_nic": "enp0s22",
    "cn_ip": "100.200.31.11",
    "cn_prefix": 16
  }'
```

### `POST /cube/cloudinit/ccvm/generate`

`cluster.json` 기반으로 CCVM cloud-init ISO를 생성한다.

고정/자동 입력:

| 항목 | 값 |
| --- | --- |
| hostname | `ccvm` |
| ISO path | `/var/lib/libvirt/images/ccvm-cloudinit.iso` |
| mgmt NIC | `enp0s20` |
| mgmt IP | `clusterConfig.ccvm.ip` |
| mgmt prefix/gw/dns | `clusterConfig.mngtNic.cidr/gw/dns` |
| 복사 대상 | `clusterConfig.hosts[].ablecubePn` |

서비스 네트워크가 없으면 빈 body로 호출할 수 있다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/cloudinit/ccvm/generate \
  -H "Content-Type: application/json" \
  -d '{}'
```

서비스 네트워크가 있으면 `sn_*` 값을 전달한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/cloudinit/ccvm/generate \
  -H "Content-Type: application/json" \
  -d '{
    "sn_nic": "enp0s21",
    "sn_ip": "172.16.31.10",
    "sn_prefix": 24,
    "sn_gw": "172.16.31.1",
    "sn_dns": "8.8.8.8"
  }'
```

### `POST /cube/cloudinit/scvm/generate`

현재 노드가 `cluster.json`의 어느 `hosts[]` 항목인지 hostname/IP로 식별하고 SCVM cloud-init ISO를 생성한다.

고정/자동 입력:

| 항목 | 값 |
| --- | --- |
| hostname | `scvm` + `hosts[].index` |
| ISO path | `/var/lib/libvirt/images/scvm-cloudinit.iso` |
| mgmt NIC | `enp0s20` |
| mgmt IP | 현재 노드 `hosts[].scvmMngt` |
| PN NIC/IP | `enp0s21`, 현재 노드 `hosts[].scvm` |
| CN NIC/IP | `enp0s22`, 현재 노드 `hosts[].scvmCn` |
| prefix/gw/dns | `clusterConfig.mngtNic` |

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/cloudinit/scvm/generate
```

## CCVM API

### `GET /cube/ccvm/status`

CCVM 상태를 조회한다. libvirt/qemu-guest-agent 정보를 함께 사용한다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/ccvm/status
```

### `POST /cube/ccvm/xml`

Cloud Center VM libvirt XML을 생성한다. XML 템플릿은 `/etc/ablestack/xml-template/ccvm-xml-template.xml`을 사용하고, 생성된 XML은 `/etc/ablestack/vmconfig/ccvm/ccvm.xml`에 저장된다.

cluster type별 disk 처리:

| Type | disk XML |
| --- | --- |
| `ablestack-hci` | RBD `rbd/ccvm`, Ceph secret UUID `11111111-1111-1111-1111-111111111111` |
| `ablestack-hci-filesystem` | HCI와 동일 |
| `ablestack-vm` | `gfs_mount_point/ccvm.qcow2` 파일 disk. `gfs_mount_point` 필수 |
| `ablestack-standalone` | `/mnt/glue/ccvm.qcow2` |
| 기타/기본 | `/var/lib/libvirt/images/ablestack-template.qcow2` |

배포 흐름:

| 단계 | 설명 |
| --- | --- |
| HCI secret | HCI/HCI-filesystem일 때만 `hosts[].ablecube` 대상에 secret 생성 API fan-out |
| XML 생성 | 요청 받은 노드에서 템플릿을 렌더링 |
| XML 설치 | `hosts[].ablecubePn` 대상의 `/etc/ablestack/vmconfig/ccvm/ccvm.xml`로 fan-out |
| 서비스 네트워크 | `service_network_bridge`가 있으면 두 번째 NIC XML 추가 |

`ablestack-vm` 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/xml \
  -H "Content-Type: application/json" \
  -d '{
    "cpu": 4,
    "memory": 16,
    "gfs_mount_point": "/mnt/glue-gfs",
    "management_network_bridge": "br0",
    "service_network_bridge": "br1"
  }'
```

HCI 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/xml \
  -H "Content-Type: application/json" \
  -d '{
    "cpu": 4,
    "memory": 16,
    "management_network_bridge": "br0"
  }'
```

### `POST /cube/ccvm/lifecycle`

Cloud Center VM lifecycle 작업을 수행한다.

지원 action:

| Action | 설명 |
| --- | --- |
| `setup` | CCVM setup |
| `reset` | cluster type에 따라 CCVM/PCS/GFS/local disk 초기화 |
| `copy` | CCVM 관련 파일 복사 |
| `start` | CCVM 시작 |
| `stop` | CCVM 종료. `destroy=true`면 강제 destroy 사용 |
| `restart` | CCVM 재시작 |
| `delete` | CCVM 삭제. `purge=true`면 이미지까지 삭제 |

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/lifecycle \
  -H "Content-Type: application/json" \
  -d '{"action":"start"}'
```

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/lifecycle \
  -H "Content-Type: application/json" \
  -d '{"action":"delete","purge":true}'
```

### `POST /cube/ccvm/edit`

CCVM이 정지 중일 때 CPU/메모리 값을 XML에 반영한다.

cluster type별 동작:

| Type | 동작 |
| --- | --- |
| `ablestack-vm` | `/mnt/glue-gfs/ccvm.xml` 공유 파일 수정 |
| `ablestack-standalone` | `/mnt/glue/ccvm.xml` 로컬 파일 수정 |
| `ablestack-hci` | 각 ablecube의 `/etc/ablestack/vmconfig/ccvm/ccvm.xml`로 fan-out |
| `ablestack-hci-filesystem` | HCI와 동일하게 각 host 로컬 XML 수정 |

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/edit \
  -H "Content-Type: application/json" \
  -d '{"cpu":"16","memory":"32"}'
```

### `POST /cube/ccvm/secondary/resize`

CCVM secondary 용량을 1~500 GiB 범위에서 추가한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/secondary/resize \
  -H "Content-Type: application/json" \
  -d '{"add_size":100}'
```

### `POST /cube/ccvm/service/control`

CCVM의 서비스 제어 요청을 CCVM 노드로 전달한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/service/control \
  -H "Content-Type: application/json" \
  -d '{"action":"restart","service_name":"mold"}'
```

지원 action: `start`, `restart`, `stop`, `status`

## CCVM Snapshot API

### `POST /cube/ccvm/snap`

HCI/HCI-filesystem 환경에서 RBD 기반 CCVM snapshot을 관리한다.

| Action | 설명 |
| --- | --- |
| `list` | snapshot 목록 |
| `backup` | snapshot 생성. `snap_name` 생략 가능 |
| `rollback` | 지정 snapshot으로 rollback. CCVM 정지 상태 필요 |

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/snap \
  -H "Content-Type: application/json" \
  -d '{"action":"list"}'
```

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/snap \
  -H "Content-Type: application/json" \
  -d '{"action":"rollback","snap_name":"auto-2026-04-30"}'
```

자동 snapshot backup은 `systemProfile.bootstrap.ccvm=true` 조건에서 API 서버 내부 스케줄러가 수행한다. 실제 백업은 PCS current DC 노드에서만 실행되도록 구성되어 있다.

## CCVM File Backup/Restore API

### `POST /cube/ccvm/backup`

VM/Standalone 계열에서 virsh backup 기반 CCVM 파일 백업과 스케줄을 관리한다.

| Action | 설명 |
| --- | --- |
| `backup` | 즉시 백업 생성 |
| `status` | 백업 작업 상태 |
| `list` | 백업 파일 목록 |
| `overview` | 스케줄/삭제 정책/파일 목록 요약 |
| `schedule` | 정기 백업 스케줄 설정 |
| `unschedule` | 정기 백업 비활성화 |
| `schedule-delete` | 오래된 백업 삭제 스케줄 설정 |
| `unschedule-delete` | 삭제 스케줄 비활성화 |

즉시 백업:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/backup \
  -H "Content-Type: application/json" \
  -d '{"action":"backup","target_dir":"/mnt/glue-gfs/backup/ccvm"}'
```

일간 스케줄:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/backup \
  -H "Content-Type: application/json" \
  -d '{"action":"schedule","repeat":"daily","time":"01:00"}'
```

### `POST /cube/ccvm/restore`

백업 파일로 CCVM 디스크를 복구한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/ccvm/restore \
  -H "Content-Type: application/json" \
  -d '{"target_file":"ccvm.qcow2-20260430_010000"}'
```

## SCVM API

### `GET /cube/scvm/status`

Storage Center VM 상태를 조회한다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/scvm/status
```

### `POST /cube/scvm/xml`

Storage Center VM libvirt XML과 hugepage 설정을 생성한다. XML 템플릿은 `/etc/ablestack/xml-template/scvm-xml-template.xml`을 사용하고, 생성 결과는 `/etc/ablestack/vmconfig/scvm/scvm.xml`이다.

생성 시 함께 처리하는 파일:

| 대상 | 설명 |
| --- | --- |
| `/etc/security/limits.conf` | `limits-template.conf`의 `{memory}` 값을 `memory * 1024 * 1024`로 치환 |
| `/etc/sysctl.conf` | `sysctl-template.conf`의 `{memory}` 값을 `memory * 1024`로 치환 |
| `/etc/ablestack/vmconfig/scvm/scvm.xml` | SCVM libvirt XML |

disk type:

| 값 | 필요 필드 | 설명 |
| --- | --- | --- |
| `lun_passthrough` | `lun_passthrough_list` | block device를 SCSI LUN으로 XML에 추가 |
| `raid_passthrough` | `raid_passthrough_list` | RAID PCI address를 hostdev로 XML에 추가 |

storage traffic network type:

| 값 | 필요 필드 | 설명 |
| --- | --- | --- |
| `bridge` | `server_network_bridge`, `replication_network_bridge` | bridge interface 2개 생성 |
| `nic_passthrough` | `server_nic_passthrough`, `replication_nic_passthrough` | PCI NIC 2개 passthrough |
| `nic_passthrough_bonding` | 각 bonding list 2개씩 | server/replication 각각 2개 NIC passthrough |

bridge 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/scvm/xml \
  -H "Content-Type: application/json" \
  -d '{
    "cpu": 4,
    "memory": 16,
    "disk_type": "lun_passthrough",
    "lun_passthrough_list": ["/dev/disk/by-id/wwn-0x1234"],
    "management_network_bridge": "br0",
    "storage_traffic_network_type": "bridge",
    "server_network_bridge": "br1",
    "replication_network_bridge": "br2"
  }'
```

NIC passthrough 예:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/scvm/xml \
  -H "Content-Type: application/json" \
  -d '{
    "cpu": 4,
    "memory": 16,
    "disk_type": "raid_passthrough",
    "raid_passthrough_list": ["00:1f.2"],
    "management_network_bridge": "br0",
    "storage_traffic_network_type": "nic_passthrough",
    "server_nic_passthrough": "0000:03:00.0",
    "replication_nic_passthrough": "0000:04:00.0"
  }'
```

### `POST /cube/scvm/lifecycle`

Storage Center VM lifecycle 작업을 수행한다. 오케스트레이터가 `cluster.json`의 대상 정보를 보고 로컬 또는 원격 노드 API를 호출한다.

지원 action:

| Action | 설명 |
| --- | --- |
| `setup` | SCVM setup |
| `reset` | SCVM reset |
| `start` | SCVM 시작 |
| `stop` | SCVM 정지 |
| `delete` | SCVM 삭제 |
| `resource` | CPU/메모리 변경. `cpu` 또는 `memory` 필요 |

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/scvm/lifecycle \
  -H "Content-Type: application/json" \
  -d '{"action":"start","target":"10.10.31.2"}'
```

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/scvm/lifecycle \
  -H "Content-Type: application/json" \
  -d '{"action":"resource","cpu":4,"memory":16}'
```

## PCS API

### `POST /cube/pcs/control`

Cloud Center PCS cluster/resource를 제어한다.

지원 action:

| Action | 설명 | 주요 필드 |
| --- | --- | --- |
| `setup` | cluster.json 기준 PCS setup | 내부 설정 |
| `setup-cron` | CCVM snapshot cron 배포 | 내부 설정 |
| `config` | PCS cluster 구성 | `cluster`, `hosts` |
| `create` | CCVM resource 생성 | `resource`, `xml` |
| `enable` | CCVM resource enable | `resource` |
| `disable` | CCVM resource disable | `resource` |
| `move` | CCVM resource 이동 | `target` |
| `cleanup` | PCS cleanup | `resource` |
| `status` | PCS 상태 조회 | `resource` |
| `remove` | PCS resource 제거 | `resource` |
| `destroy` | PCS cluster 삭제 | 없음 |
| `stop` | PCS cluster 정지 | 없음 |
| `sync` | totem token 시간 설정 | `time` |
| `ccvm-status` | libvirt에 CCVM domain 생성 여부 확인 | 없음 |

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/pcs/control \
  -H "Content-Type: application/json" \
  -d '{"action":"status"}'
```

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/pcs/control \
  -H "Content-Type: application/json" \
  -d '{"action":"move","target":"ablecube31-2"}'
```

## Glue and GFS API

### `GET /cube/gluecluster/status`

Glue cluster 상세 상태를 조회한다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/gluecluster/status
```

### `POST /cube/gluecluster/update`

유지보수 모드를 설정하거나 해제한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/gluecluster/update \
  -H "Content-Type: application/json" \
  -d '{"action":"set_noout"}'
```

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/gluecluster/update \
  -H "Content-Type: application/json" \
  -d '{"action":"unset_noout"}'
```

### `GET /cube/gfs/resource/status`

`pcs status xml` 기반으로 GFS 관련 PCS 리소스 상태를 조회한다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/gfs/resource/status
```

### `GET /cube/gfs/disk/status`

GFS2로 마운트된 디스크 목록과 multipath/single mode 정보를 조회한다.

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/cube/gfs/disk/status
```

## DB Dump API

### `POST /cube/db/dump`

CCVM의 `cloud` DB dump와 백업/삭제 스케줄을 관리한다.

| Action | 설명 | 주요 필드 |
| --- | --- | --- |
| `instantBackup` | 즉시 dump 생성 | `path` |
| `regularBackup` | 정기 백업 스케줄 등록 | `path`, `repeat`, `timeone`, `timetwo` |
| `deleteOldBackup` | 오래된 dump 삭제 스케줄 등록 | `path`, `repeat`, `timeone`, `delete` |
| `checkBackup` | 백업/삭제 스케줄 조회 | `checkOption` |
| `deactiveBackup` | 백업/삭제 스케줄 비활성화 | `checkOption` |

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/db/dump \
  -H "Content-Type: application/json" \
  -d '{"action":"instantBackup","path":"/home/db_backup"}'
```

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/db/dump \
  -H "Content-Type: application/json" \
  -d '{"action":"regularBackup","path":"/home/db_backup","repeat":"daily","timeone":"02:00"}'
```

`repeat` 값:

| 값 | 의미 |
| --- | --- |
| `no` | 1회성 at job |
| `hourly` | 매시간 |
| `daily` | 매일 |
| `weekly` | 매주. `timetwo`는 0~6 weekday |
| `monthly` | 매월. `timetwo`는 `월간격-일`, 예: `1-15` |

`checkOption` 값:

| 값 | 의미 |
| --- | --- |
| `r` | 백업 스케줄 |
| `d` | 삭제 스케줄 |

## License API

### `POST /cube/license`

라이선스를 조회하거나 등록한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/license \
  -H "Content-Type: application/json" \
  -d '{"action":"status"}'
```

등록:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/license \
  -H "Content-Type: application/json" \
  -d '{"action":"register","license_content":"BASE64_CONTENT","original_filename":"license-file-name"}'
```

## Version Update API

### `POST /cube/version/update`

마운트된 ABLESTACK ISO에서 버전 정보를 조회하거나 `update.sh`를 실행한다.

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/version/update \
  -H "Content-Type: application/json" \
  -d '{"action":"info","mount_path":"/mnt/ablestack-iso"}'
```

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/version/update \
  -H "Content-Type: application/json" \
  -d '{"action":"run","mount_path":"/mnt/ablestack-iso"}'
```

`mount_path`는 절대 경로여야 하며, 내부에 `ks/ablestack-ks.cfg`와 `update.sh`가 있어야 한다.

## Security Patch API

### `POST /cube/security/patch`

`cluster.json` 대상에 `security_patch.sh`를 로컬/SSH로 실행하거나 `security_patch.status` 값을 업데이트한다.

기본 실행:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/security/patch \
  -H "Content-Type: application/json" \
  -d '{"targets":["all"],"ssh_user":"root","ssh_port":22}'
```

SSH 포트 변경:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/security/patch \
  -H "Content-Type: application/json" \
  -d '{"port_change":true,"new_port":10022,"targets":["all"],"ssh_user":"root","ssh_port":22}'
```

Ceph SSH 설정 변경:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/security/patch \
  -H "Content-Type: application/json" \
  -d '{"ceph_ssh_change":true,"new_port":10022}'
```

추가 호스트용 실행:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/security/patch \
  -H "Content-Type: application/json" \
  -d '{"add_host":true,"new_port":10022,"ssh_user":"root","ssh_port":22}'
```

JSON status 업데이트:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/cube/security/patch \
  -H "Content-Type: application/json" \
  -d '{"update_json_file":true,"local":false}'
```

## Legacy/Status APIs

### Neighbor

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/neighbor
curl -sS http://<ablecube-ip>:8090/api/v1/neighbor/info
```

등록:

```bash
curl -X POST http://<ablecube-ip>:8090/api/v1/neighbor \
  -H "Content-Type: application/json" \
  -d '{"ip":"10.10.1.1","hostname":"ablecube11"}'
```

### Glue

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/glue/
curl -sS http://<ablecube-ip>:8090/api/v1/glue/auth
curl -sS http://<ablecube-ip>:8090/api/v1/glue/auth/client.admin
curl -sS http://<ablecube-ip>:8090/api/v1/glue/auths
```

`/glue/auth`는 username을 생략하면 기본적으로 `client.admin`을 조회한다. 특정 entity를 조회하려면 `/glue/auth/:username` 경로를 사용한다.

### Mold

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/mold
curl -sS http://<ablecube-ip>:8090/api/v1/mold/ccvm
```

### PCS Status

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/pcs
curl -sS http://<ablecube-ip>:8090/api/v1/pcs/resources
```

### Dashboard

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/dashboard
```

### Error Log

```bash
curl -sS http://<ablecube-ip>:8090/api/v1/err
curl -X DELETE http://<ablecube-ip>:8090/api/v1/err
```

## Background Jobs

`cmd/apiserver/main.go`에서 `controller`에 등록되는 주기 작업이다. 컨트롤러는 10초 주기로 등록된 handler를 실행한다.

| Job | 등록 함수 | 설명 |
| --- | --- | --- |
| Glue monitor | `Glue.Monitor` | Glue status, health, auth, daemon, storage size, version 갱신 |
| PCS monitor | `PCS.Monitor` | PCS status 갱신 |
| Hosts cache | `CubeHandler.UpdateHosts` | `/etc/hosts` 캐시 갱신 |
| Cluster config cache | `CubeHandler.UpdateClusterConfig` | `cluster.json` 캐시 갱신 |
| SSH known_hosts scan | `CubeHandler.AutoSSHKnownHostsScan` | 하루 1회 조건으로 대상 host key scan |
| CCVM snapshot backup | `CubeHandler.AutoCCVMSnapshotBackup` | CCVM snapshot 자동 백업 |
| CCVM file backup schedule | `CubeHandler.AutoCCVMFileBackupSchedule` | CCVM 파일 백업 스케줄러 |
| NIC cache | `CubeHandler.UpdateNICs` | NIC 목록 갱신 |
| Disk cache | `CubeHandler.UpdateDisk` | Disk 목록 갱신 |
| Config save | `C.SaveConfig` | neighbor config 저장 |

## 구현상 주의 사항

| 항목 | 현재 코드 기준 |
| --- | --- |
| SCVM lifecycle 경로 | `/cube/scvm/lifecycle` |
| System Profile 조회 | `/cube/system/config` |
| Cluster Config 조회 | `/cube/cluster/config` |
| CCVM secondary resize | `/cube/ccvm/secondary/resize` |
| CCVM/SCVM XML 생성물 | `/etc/ablestack/vmconfig/ccvm/ccvm.xml`, `/etc/ablestack/vmconfig/scvm/scvm.xml` |
| `ccvm` network config | `clusterConfig.ccvm.ip`만 사용. CIDR/GW/DNS는 `clusterConfig.mngtNic` 사용 |
| optional runtime command | `pcs`, `virsh`, `ceph`, `rbd`, `genisoimage` 등은 RPM 설치를 막지 않고 해당 API 실행 시점에 필요 |

## 운영 전 보완 예정

운영 배포 전에는 `docs/post-development-review-notes.md`의 항목을 기준으로 API 인증, 내부 fan-out 토큰, CORS 제한, secret 제거, 백그라운드 작업 중복 실행 방지 등을 반영한다.
