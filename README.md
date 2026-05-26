# ABLESTACK API

ABLESTACK API는 Cube 노드에서 실행되는 Go 기반 관리 API 서버입니다. 클러스터 구성, 호스트/디스크/NIC 조회, CCVM/SCVM lifecycle, PCS 제어, Glue 상태 조회, 라이선스, 보안 패치, DB/VM 백업 같은 Cube 운영 기능을 HTTP API로 제공합니다.

## What This API Does

| 영역 | 주요 기능 |
| --- | --- |
| Cluster | `cluster.json` 조회/적용, 호스트 추가/삭제, systemProfile 관리, 노드 health check |
| Host Inventory | `/etc/hosts`, 디스크, NIC, GFS 디스크/리소스 상태 조회 |
| Cloud Center VM | CCVM 상태 조회, 시작/정지/삭제/초기화, CPU/메모리 수정, secondary 용량 추가 |
| Storage Center VM | SCVM 상태 조회, 시작/정지/삭제/setup/reset, CPU/메모리 수정 |
| PCS/Glue | PCS cluster/resource 제어, Glue cluster 상태 조회, 유지보수 모드 설정/해제 |
| Backup | CCVM snapshot, CCVM 파일 백업/복구, CCVM DB dump/스케줄 관리 |
| Operations | 라이선스 등록/조회, ABLESTACK 버전 업데이트, 보안 패치 실행 |

## Repository Structure

```text
cmd/apiserver/                 API 서버 진입점, Gin 라우팅, Swagger 노출
configs/                       API 서버 기본 설정 파일
docs/                          Swagger 산출물과 상세 운영 문서
internal/handler/              HTTP 요청 처리 계층
internal/model/                요청/응답 DTO, 캐시 모델, 상태 모델
internal/service/controller/   주기 실행 컨트롤러, neighbor, 설정 저장
internal/service/clusterconfig/cluster.json 및 /etc/hosts 적용 로직
internal/infra/utils/          공통 HTTP/SSH/error 유틸리티
shell/                         보안 패치 shell script
```

## Quick Start

현재 서버 listen port는 `cmd/apiserver/main.go` 기준 `8090`으로 고정되어 있습니다.

```bash
go run ./cmd/apiserver
```

기본 생존 확인:

```bash
curl -sS http://127.0.0.1:8090/api/v1/cube/cluster/health
```

Swagger UI:

```text
http://<ablecube-ip>:8090/swagger/index.html
```

## Runtime Requirements

이 API는 단순 웹 API가 아니라 Cube 노드의 시스템 명령을 호출합니다. 운영 노드에는 기능별로 다음 명령/서비스가 필요할 수 있습니다.

| 기능 | 필요 요소 |
| --- | --- |
| libvirt 기반 VM 상태/제어 | `libvirt`, `virsh`, `qemu-guest-agent`, Go libvirt 빌드 환경 |
| PCS 제어 | `pcs`, `crm_mon`, pacemaker/corosync |
| Glue/Ceph 상태 | `ceph`, `rbd` |
| 디스크/NIC 조회 | `lsblk`, `lspci`, `nmcli`, `ip` |
| DB dump | `/usr/bin/mysqldump`, `crontab`, `at` |
| SSH key scan/security patch | `ssh`, `ssh-keyscan`, `ssh-keygen`, `security_patch.sh` |

macOS 개발 환경에서 `go test ./...`를 실행하려면 libvirt CGO 의존성 때문에 `pkg-config`와 libvirt 개발 패키지가 필요합니다.

## Configuration

| 변수 | 용도 |
| --- | --- |
| `CUBE_CONFIG_PATH` | neighbor 설정 파일 경로. 기본값은 `configs/config.json` |
| `ABLESTACK_CLUSTER_JSON` | `cluster.json` 절대 경로 override |
| `ABLESTACK_CONFIG_PATH` | ABLESTACK 설정 루트. 기본값은 `/etc/ablestack` |
| `ABLESTACK_STATE_PATH` | VM 설정 생성물 루트. 기본값은 `/etc/ablestack/vmconfig` |
| `ABLESTACK_API_SCHEME` | 노드 간 API 호출 scheme. 기본값 `http` |
| `ABLESTACK_API_PORT` | 노드 간 API 호출 대상 port. 기본값 `8090` |
| `ABLESTACK_SECURITY_PATCH_SCRIPT` | 보안 패치 스크립트 경로 override |

## RPM Build

```bash
VERSION=0.1.0 RELEASE=1 ./scripts/build-rpm.sh
```

빌드 결과는 `dist/rpm/rpmbuild/RPMS`와 `dist/rpm/rpmbuild/SRPMS` 아래에 생성됩니다. RPM은 `cmd/apiserver/main.go`를 `/usr/bin/ablestack-api`로 빌드하고 `ablestack-api.service`를 설치한 뒤 `systemctl enable --now ablestack-api.service`를 실행합니다. `firewall-cmd`가 있는 환경에서는 `firewalld`를 `enable --now` 처리하고 API 포트 `8090/tcp`를 runtime/permanent 모두 열어줍니다.

RPM 설치 경로:

| 항목 | 경로 |
| --- | --- |
| API binary | `/usr/bin/ablestack-api` |
| systemd unit | `/usr/lib/systemd/system/ablestack-api.service` |
| service env | `/etc/ablestack/ablestack-api.env` |
| runtime config | `/etc/ablestack/config.json` |
| cluster properties | `/etc/ablestack/properties` |
| XML templates | `/etc/ablestack/xml-template` |
| shell resources | `/etc/ablestack/shell` |
| generated VM config | `/etc/ablestack/vmconfig` |

`/etc/ablestack` 아래 설정 파일은 RPM spec에서 `%config(noreplace)`로 관리합니다. 업데이트 시 기존 파일은 덮어쓰지 않고, `config.json`과 `properties/cluster.json`은 새 RPM의 `.rpmnew`가 생기면 누락된 JSON key만 병합합니다. 기존 값과 운영 데이터는 유지됩니다.

## API Base URL

```text
http://<ablecube-ip>:8090/api/v1
```

예시:

```bash
curl -sS http://10.10.12.1:8090/api/v1/cube/disk?action=list
```

## Main API Groups

| Group | Endpoint 예시 | 설명 |
| --- | --- | --- |
| Cluster | `GET /cube/cluster/health`, `POST /cube/cluster/apply` | 클러스터 구성/상태 관리 |
| System Profile | `GET/POST /cube/system/config` | `cluster.json`의 `systemProfile` 조회/수정 |
| Host | `GET /cube/hosts` | `/etc/hosts`를 역할/네트워크 기준으로 조회 |
| Disk/NIC | `GET /cube/disk`, `GET /cube/nics` | 노드 디스크/NIC 인벤토리 |
| CCVM | `GET /cube/ccvm/status`, `POST /cube/ccvm/lifecycle` | Cloud Center VM 관리 |
| SCVM | `GET /cube/scvm/status`, `POST /cube/scvm/lifecycle` | Storage Center VM 관리 |
| PCS | `POST /cube/pcs/control` | Cloud Center PCS 리소스 제어 |
| Glue/GFS | `GET /cube/gluecluster/status`, `GET /cube/gfs/disk/status` | 스토리지 클러스터 상태 |
| Backup | `POST /cube/ccvm/snap`, `POST /cube/ccvm/backup`, `POST /cube/db/dump` | snapshot, 파일 백업, DB 백업 |
| Operations | `POST /cube/license`, `POST /cube/version/update`, `POST /cube/security/patch` | 운영/유지보수 작업 |

## Documentation

- [API 실행 및 상세 설명](docs/api-execution-and-description.md)
- [개발 완료 후 보안/구조 개선 메모](docs/post-development-review-notes.md)
- Swagger: `/swagger/index.html`

## Cluster Fan-out Model

일부 API는 요청을 받은 노드가 오케스트레이터 역할을 하고, `cluster.json`의 host 정보를 기준으로 다른 ablecube/scvm/ccvm 대상에 내부 API를 호출합니다.

대표 예:

```text
POST /api/v1/cube/cluster/apply
  -> 각 대상 노드의 /api/v1/cube/cluster/apply-local 호출
```

현재 구조를 유지하려면 3대 호스트 사이에 `8090/tcp` 통신이 가능해야 합니다. 개발 완료 후 운영 안정화 단계에서는 내부 호출용 토큰을 추가해 외부 사용자 호출과 노드 간 호출을 분리하는 방향으로 개선합니다.
