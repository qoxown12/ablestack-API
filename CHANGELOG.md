# CHANGELOG

ABLESTACK API의 변경 이력은 이 파일에 기록한다. RPM 버전은 `VERSION` 파일을 기준으로 관리한다.

## [0.1.4] - 2026-06-08

### Added

- SCVM/CCVM bootstrap을 단독 실행할 수 있도록 `/cube/scvm/bootstrap`, `/cube/ccvm/bootstrap` API를 추가했다. 각 API는 host qemu-guest-agent로 VM 내부 `/root/bootstrap.sh`를 실행한 뒤 VM API health와 라이선스 후처리를 수행한다.
- 기존 `multipath_sync.sh` 흐름을 대체할 수 있도록 SSH/SCP 없이 host API fan-out으로 SCSI rescan과 multipath bindings/wwids 동기화를 수행하는 `/cube/multipath/sync` API를 추가했다.
- `/cube/version/update`에 `update_type=all,mold`를 추가해 전체 업데이트(`update-all.sh`)와 Mold 업데이트(`update-mold.sh`)를 선택 실행할 수 있도록 했다.

### Changed

- SCVM Swagger `doc.json`은 Glue 중심 화면이 되도록 Cube 운영/내부 통신 API path/tag를 숨기고, 인증, health, version, license 계열 API만 남기도록 변경했다.
- SCVM Swagger tag 순서를 Glue 계열이 먼저 보이도록 재정렬했다.
- Swagger 문서 필터링 후 사용하지 않는 model definition을 제거해 host/CCVM과 SCVM Swagger 화면에 역할과 무관한 schema가 남지 않도록 정리했다.
- `/cube/deploy/run`의 `scvm_bootstrap`, `ccvm_bootstrap` step이 별도 bootstrap API와 같은 실행 함수를 사용하도록 정리했다.
- SCVM bootstrap 스크립트는 Ceph bootstrap 중복 실행을 피하기 위해 대표 SCVM 1대에서만 실행하고, 라이선스 등록/status 확인은 전체 SCVM 대상으로 유지하도록 변경했다.
- bootstrap 스크립트 실행 후 라이선스 후처리만 재시도할 수 있도록 직접 bootstrap API에는 `run_script`, `/cube/deploy/run`에는 `run_bootstrap_script` 옵션을 추가했다.
- `/cube/version/update`는 ISO 마운트 경로를 직접 실행하지 않고 `/opt/ABLESTACK_UPDATE`로 복사한 뒤 작업 디렉터리에서 스크립트를 실행하도록 변경했다.
- `/cube/version/update`의 `info` 응답에 현재/대상 OS 버전과 Mold 버전을 포함하고, 대상 Mold 버전은 `AppStream/Packages/mold` RPM 파일명에서 버전명과 날짜까지만 우선 추출하도록 변경했다.
- `/cube/version/update`의 `run`은 `/cube/deploy/status`가 `stage=ready`인 경우에만 실행되도록 서버 측 조건을 추가했다.
- `/cube/gfs/disk/status`의 `blockdevices[]`에 `df -hP` 기준 `used`, `avail`, `use_percent`를 mountpoint별로 추가했다.
- `cluster apply` insert 흐름에서 요청 body의 `security.internal_token`이 있으면 그 값을 사용하고, 없으면 기존 token을 보장하거나 새로 생성해 apply-local payload로 전파하도록 정리했다.
- `/cube/cluster/config` 응답이 다운로드용 cluster.json에 필요한 `clusterConfig`와 `security`를 함께 반환하고 `systemProfile`은 제외하도록 변경했다.
- `clusterConfig.iscsi_storage` 필드명을 `storage_network`로 변경하고, 기존 `iscsi_storage` 입력은 호환용으로 `storage_network`로 정규화하도록 했다.

### Fixed

- `/cube/deploy/status`에서 SCVM/CCVM bootstrap flag가 이미 true인 경우에도 VM running 상태를 raw 응답에 계속 반환하도록 수정했다.
- `/cube/gfs/disk/status`가 `/mnt/glue-gfs`, `/mnt/glue-gfs-1`처럼 여러 GFS mountpoint가 있는 환경에서 각 mountpoint의 사용량을 개별 매칭하도록 보완했다.

## [0.1.3] - 2026-06-05

### Added

- SCVM XML 생성 API에 `disk_passthrough` 디스크 타입과 `disk_passthrough_list` 입력을 추가했다.
- 라이선스 등록 API에서 `multipart/form-data` 파일 업로드를 지원해 서버가 라이선스 파일을 읽고 인코딩 처리하도록 추가했다.
- 마스터 노드에서 라이선스를 전체 ablecube 호스트로 배포할 수 있도록 `/cube/license/apply` API를 추가했다.
- 라이선스 배포, 클러스터 구성 적용, SCVM 준비, 스토리지 준비, 로컬 스토리지 준비, CCVM 준비, systemProfile 반영을 순차 실행하는 올인원 배포 job API `/cube/deploy/run`, `/cube/deploy/jobs`, `/cube/deploy/jobs/{job_id}`를 추가했다.
- 올인원 배포 API 사용 방법과 HCI/HCI Filesystem/VM/Standalone 타입별 실행 흐름을 `docs/deploy-run-guide.md`에 추가했다.
- 모든 API 요청과 주요 action 로그를 `/var/log/ablestack/api.log`에, 오류 상세 로그를 `/var/log/ablestack/detail.log`에 남기도록 추가했다.
- background job 실패, 상태 변화, 자동 백업 실행 결과를 `/var/log/ablestack/job.log`에 남기도록 추가했다.
- 날짜가 지난 로그를 `/var/log/ablestack/archive`에 `api.log-YYYY-MM-DD.gz`, `detail.log-YYYY-MM-DD.gz`, `job.log-YYYY-MM-DD.gz` 형식으로 일 단위 압축 보관하고 기본 90일 보관 후 정리하도록 했다.
- `/api/v1/glue` namespace와 `internal/handler/glue`, `internal/model/glue` 골격을 추가하고, Glue API route를 SCVM role에서만 등록하도록 제한했다.
- `internal/service/glueservice`를 추가하고 `/api/v1/glue/status`, `/hosts`, `/version`, `/pool`, `/image`, `/service`를 SCVM 로컬 `ceph`, `rbd`, `ceph orch` 명령 기반 실제 실행 API로 연결했다.
- `/api/v1/glue/gluefs`, `/nfs`, `/rgw`의 조회 계열 endpoint를 SCVM 로컬 `ceph`, `ceph nfs`, `ceph orch`, `radosgw-admin` 명령 기반 실제 실행 API로 연결했다.
- `/api/v1/glue/gluefs`, `/nfs`, `/rgw`의 생성/수정/삭제 endpoint를 SCVM 로컬 `ceph`, `ceph nfs`, `ceph orch`, `radosgw-admin`, Ceph dashboard API 기반 실제 실행 API로 연결하고, NFS ingress spec 적용/재배포 흐름을 추가했다.
- `/api/v1/glue/nvmeof` endpoint를 SCVM 로컬 `ceph orch`, `rbd`, `podman run/exec` 기반 실제 실행 API로 연결했다.
- `/api/v1/glue/iscsi` endpoint를 SCVM 로컬 `ceph orch`, Ceph dashboard API, `podman exec gwcli` 기반 실제 실행 API로 연결했다.
- `/api/v1/glue/smb` endpoint를 SCVM 로컬 Samba 실행 스크립트 기반 실제 실행 API로 연결하고, SMB password가 실패 응답에 노출되지 않도록 마스킹했다.
- `/api/v1/glue/mirror` endpoint를 SCVM 로컬 `rbd mirror`, `ceph orch`, bootstrap token import/export 기반 실제 실행 API로 연결했다.
- 기존 glue-api의 `Samba-Execute.sh`와 `smb_conf` helper를 `shell/` 리소스로 포함해 RPM 설치 시 `/etc/ablestack/shell/` 아래에 배치되도록 추가했다.
- SCVM SMB API 테스트 체크리스트를 `docs/scvm-smb-test-checklist.md`에 추가했다.

### Changed

- 라이선스 등록 성공 시 `cluster.json`의 `systemProfile.license.status=true`와 함께 복호화된 라이선스 `oem` 값을 `systemProfile.license.type`에 자동 반영하도록 변경했다.
- NVMe-oF 흐름에서 legacy SSH 기반 실행을 제거하고 SCVM 로컬 실행만 사용하도록 변경했다.
- iSCSI purge 흐름에서 legacy SSH 기반 실행을 제거하고 SCVM 로컬 실행만 사용하도록 변경했다.
- SMB 흐름에서 legacy SSH host 반복을 제거하고 SCVM 로컬 실행만 사용하도록 변경했다.
- SMB 기본 실행 경로를 기존 glue-api 경로에서 `/etc/ablestack/shell/Samba-Execute.sh`로 변경했다.
- `Samba-Execute.sh`를 action별 함수 구조와 flag parser로 정리하고, 필수 값/의존 명령/설정 파일 오류를 명확한 exit code와 stderr 메시지로 반환하도록 개선했다.
- Samba/Ceph/realmd 같은 SCVM 전용 runtime command는 API RPM hard dependency가 아니라 role 이미지/패키지에서 보장하는 기준으로 정리했다.
- Mirror 흐름에서 legacy SSH/scp 기반 remote cluster 직접 제어를 제거하고 bootstrap token 교환 방식으로 변경했다.
- Glue API 명령 실행에서 legacy SSH와 shell pipeline 의존을 제거하고, pool/image/service 입력값을 검증해 legacy `grep|cut` 방식과 command injection 위험을 줄였다.
- Swagger tag를 `Cube-License`, `Cube-Nic`, `Glue-RGW`, `Glue-GlueFS`처럼 기능 단위로 세분화하고, host/CCVM에서는 Swagger `doc.json`에서도 SCVM 전용 Glue path가 보이지 않도록 변경했다.
- `/cube/license/apply`에 `roles` 입력을 추가해 기존 `hosts[].ablecube` 기본 배포는 유지하면서, SCVM 생성 후 `roles:["scvm"]`, CCVM 생성 후 `roles:["ccvm"]`, 전체 API 대상에는 `roles:["all"]`로 라이선스를 fan-out 등록할 수 있도록 했다.
- 라이선스 배포 응답에 `role`을 포함해 `ablecube`, `scvm`, `ccvm`, 명시 target 대상이 구분되도록 했다.
- `/api/v1/health`와 `/health`를 라이선스/토큰 없이 호출 가능한 public health check로 추가하고, 내부 health probe가 기존 `/cube/cluster/health` 대신 이 경로를 사용하도록 변경했다.
- 올인원 배포 job에 `scvm_bootstrap`, `ccvm_bootstrap` 단계를 추가해 VM 생성 후 API health 확인, 라이선스 자동 등록, 라이선스 status 확인을 분리했다.
- SCVM/CCVM bootstrap 성공 시점에 `systemProfile.bootstrap.scvm`, `systemProfile.bootstrap.ccvm`을 true로 반영하도록 변경했다.
- CCVM cloud-init에 포함하는 `cluster.json` 경로를 API 기본 경로인 `/etc/ablestack/properties/cluster.json`로 변경했다.
- legacy `/neighbor`, `/glue`, `/mold`, `/pcs`, `/dashboard` API 라우트와 관련 handler/model 패키지를 제거하고, Cube API 중심 구조로 정리했다.
- neighbor 전용 `configs/config.json`, `CUBE_CONFIG_PATH`, `controller.SaveConfig/LoadConfig` 흐름을 제거했다.
- 라이선스 조회/등록 API는 Bearer token 없이 호출할 수 있도록 하고, 활성 라이선스가 있는 상태에서 라이선스를 교체할 때만 기존 Bearer token을 요구하도록 했다.
- API access token 서명값을 활성 라이선스의 `license_key`에서 파생하도록 변경했다.
- `ABLESTACK_AUTH_TOKEN_SECRET` 환경 변수 override를 제거해 API access token 서명값이 항상 활성 라이선스 기준으로 계산되도록 했다.
- 라이선스가 없거나 만료된 경우 라이선스 조회/등록과 Swagger를 제외한 API 요청을 차단하도록 했다.
- API 서버 내부 background job 실행 주기를 10초에서 30초로 조정했다.
- 라이선스 파일 업로드 등록에서 `original_filename` 입력을 제거하고 업로드 파일명을 그대로 저장 파일명으로 사용하도록 변경했다.
- 활성 라이선스 교체 시 Bearer token 외에 유효한 `X-Cube-Internal-Token` 내부 호출도 허용해 `/cube/license/apply` fan-out에서 원격 호스트 라이선스를 교체할 수 있도록 했다.
- 라이선스 기반 인증 구조에 맞춰 `/auth/sync`, `/auth/apply`와 `auth.json`의 `access_token_secret` 설정을 제거하고, RPM 업데이트 시 기존 `auth.json`의 legacy key도 정리하며 내부 토큰 관리는 `/auth/internal-token/*` API로 분리했다.
- `detail.log`에 라이선스 등록 실패 단계, 누락 필드, 파일명, 오류 분류 등 원인 분석용 진단 정보를 남기도록 개선했다.
- Go 모듈 기준 버전과 RPM 빌드 요구 버전을 Go 1.26.2로 상향했다.
- Go 모듈 의존성을 최신 안정 버전 기준으로 갱신했다.
- `/version` API가 OS `PRETTY_NAME`, Kernel `uname -r`, Cockpit `Version`, Mold `ACS_VERSION`, ABLESTACK 패키지 목록, HCI 계열의 `glue version` 값을 함께 반환하도록 확장하고 라우트를 활성화했다.
- `/version` API 응답에서 하드코딩된 CUBE 버전 기본값을 제거했다.

### Fixed

- `golang.org/x/net`을 `v0.55.0`으로 올려 HTTP/2 취약 의존성 경고를 해소하고, SSH 경로 보안 수정을 위해 `golang.org/x/crypto`를 `v0.52.0`으로 함께 업데이트했다.
- 라이선스 등록 API의 multipart 업로드 처리 추가 중 `err` 재선언으로 RPM 빌드가 실패하던 문제를 수정했다.

## [0.1.2] - 2026-05-27

### Added

- Cockpit 로그인 세션에서 비밀번호 재입력 없이 Bearer 토큰을 발급할 수 있도록 `ablestack-auth-token` CLI를 추가했다.
- 클러스터 전체 API 서버의 인증 서명값을 맞출 수 있도록 `/auth/sync`, `/auth/apply` API를 추가했다.
- `cluster apply` insert 시 `security.internal_token`을 생성하고 apply-local payload로 전파하도록 했다.
- `/cube/ssh/key` API를 추가해 각 호스트에서 `/root/.ssh/id_rsa`, `/root/.ssh/id_rsa.pub`, `/root/.ssh/authorized_keys`를 생성하고 관리할 수 있도록 했다.
- SSH 키를 Windows PC로 옮겨 다른 호스트에 적용할 수 있도록 암호화된 단일 파일 다운로드/업로드 흐름을 추가했다.

### Changed

- 인증 서명값은 토큰 발급 경로에서만 생성하고, API 서버 시작/토큰 검증/동기화 경로에서는 자동 생성하지 않도록 정리했다.
- `/auth/sync`는 `host`, `scvm`, `ccvm`, `all` 옵션으로 동기화 대상을 선택하고 대상별 성공/실패 결과를 반환하도록 변경했다.
- `/auth/sync`의 `all` 대상은 HCI 계열에서 `host/scvm/ccvm`, VM/standalone 계열에서 `host/ccvm`만 포함하도록 os_type 기준을 반영했다.
- 정렬된 `cluster.json` 구조에서도 auth sync 대상이 정상 수집되도록 `clusterConfig` 읽기 로직을 보완했다.
- 대상 호스트의 `security.internal_token`이 비어 있거나 다른 값이면 `/auth/sync`를 실행한 호스트의 internal token과 인증 서명값으로 덮어쓰도록 보완했다.
- cloud-init ISO 생성은 CCVM/SCVM 전용 API만 노출하고, 파일 경로와 NIC/IP를 모두 받던 저수준 `/cube/cloudinit/generate` POST API는 제거했다.
- 단일 동작만 수행하는 HBA 조회와 Glue config 동기화 POST API는 Swagger에서 요청 body를 제거하고 기존 body 입력은 호환만 유지하도록 정리했다.
- `/cube/cluster/health`의 `option`은 `host,scvm`처럼 콤마 조합을 허용하고, `target_hostname`은 host/scvm/ccvm 역할별 표시 이름을 콤마로 지정할 수 있도록 정리했다. `option` 없이 `target_hostname`만 지정하면 이름으로 role을 추론한다.
- Swagger 요청 body에서 내부 fan-out용 `security`, deprecated alias 필드를 숨겨 화면에서 불필요한 값을 보내지 않도록 정리했다.
- 클러스터 구성 `insert` 적용 성공 시 각 호스트에서 `/etc/chrony.conf` 생성과 `chronyd` 재시작을 자동 실행하도록 했다.
- 시간 서버 구성은 `clusterConfig.external_timeserver`와 `hosts[].index` 1, 2의 `ablecube` 값을 기준으로 자동 계산하도록 정리했다.
- 시간 서버 수동 재적용 endpoint는 내부/수동 용도로 유지하되 Swagger와 공개 API 설명 문서에서는 숨겼다.
- SSH 키 다운로드 파일은 키 내용을 직접 노출하지 않도록 AES-GCM으로 암호화하고 랜덤 `.dat` 파일명으로 내려주도록 했다.

## [0.1.1] - 2026-05-26

### Added

- UI 배포 흐름을 단순화할 수 있도록 `/cube/deploy/status` API를 추가했다.
- 배포 상태 응답에 `stage`, `message_key`, `severity`, `available_actions`, `warnings`, `raw`를 포함하도록 했다.
- `os_type`별로 필요한 raw 상태 값만 반환하도록 정리했다.
- CloudCenter PCS/resource 상태를 배포 상태 판단에 포함했다.
- PCS 클러스터 대상을 `pcs_cluster_list` 입력 기준으로 1~16개까지 관리할 수 있도록 확장했다.

### Changed

- 기존 화면의 sessionStorage 기반 배포 판단 로직을 백엔드에서 의미형 상태 enum으로 계산할 수 있도록 구조화했다.
- `ablestack-vm`은 PCS 대상 1대부터 허용하고, `ablestack-hci`와 `ablestack-hci-filesystem`은 PCS 대상 3대 이상을 요구하도록 검증 기준을 분리했다.
- HCI 계열에서 3대 초과 설치 시 전체 호스트가 아니라 Ceph MON 대상 노드만 PCS 목록으로 사용하도록 문서화했다.
- PCS status, setup, snapshot, Glue config 복사 경로가 `hostname1~3` 고정값 대신 동적 PCS 목록을 사용하도록 변경했다.
- Swagger와 API 설명 문서를 신규 배포 상태 API와 동적 PCS 목록 기준에 맞춰 갱신했다.

### Fixed

- CloudCenter 상태 조회가 로컬 PCS만 확인하던 흐름을 보완해 실행 가능한 PCS 노드를 선택하도록 정리했다.
- Glue config 복사 시 첫 번째 PCS 노드에만 의존하지 않고 사용 가능한 PCS 노드를 순차적으로 확인하도록 보완했다.
- Mold 상태 조회 중 fatal 종료나 재귀 호출로 API 프로세스 안정성에 영향을 줄 수 있는 부분을 정리했다.

## [0.1.0] - 2026-05-26

### Added

- RPM 빌드 기준 버전을 `VERSION` 파일로 분리했다.
- RPM 산출물에 `CHANGELOG.md`와 `VERSION`을 문서 파일로 포함한다.
- Linux 계정 기반 로그인 API와 Bearer 토큰 인증 흐름을 추가했다.
- Swagger UI에서 Bearer 토큰을 사용할 수 있도록 보안 정의와 문서 패치 스크립트를 추가했다.
- 내부 호스트 간 호출용 토큰을 `cluster.json`에서 생성, 검증, 교체할 수 있는 흐름을 추가했다.

### Changed

- RPM 설치 경로를 `/etc/ablestack` 기준으로 정리했다.
- 생성되는 VM 설정 파일 경로를 `/etc/ablestack/vmconfig`로 통일했다.
- RPM 업데이트 시 기존 설정 값을 유지하고 누락된 JSON key만 병합하도록 정리했다.
- RPM 설치 시 `firewalld`가 있으면 API 포트 `8090/tcp`를 자동으로 열도록 정리했다.
- Go module import 경로를 GitHub 저장소명 대신 `ablecloud.io/ablestack-api` 기준으로 변경했다.
- `internal/handler/cube` 파일명을 기능 기준으로 정리했다.

### Fixed

- RPM 빌드 시 이전 파일명에 남아 있던 import 경로 때문에 실패하던 문제를 정리했다.
- `configs/config.json` 누락으로 서비스 시작 중 panic이 발생하던 경로 문제를 정리했다.
- `ccvm_secondary_resize.go`의 libvirt helper 참조 오류를 정리했다.
