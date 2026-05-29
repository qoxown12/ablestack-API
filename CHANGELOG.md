# CHANGELOG

ABLESTACK API의 변경 이력은 이 파일에 기록한다. RPM 버전은 `VERSION` 파일을 기준으로 관리한다.

## [0.1.2] - 2026-05-27

### Added

- Cockpit 로그인 세션에서 비밀번호 재입력 없이 Bearer 토큰을 발급할 수 있도록 `ablestack-auth-token` CLI를 추가했다.
- 클러스터 전체 API 서버의 인증 서명값을 맞출 수 있도록 `/auth/sync`, `/auth/apply` API를 추가했다.
- `cluster apply` insert 시 `security.internal_token`을 생성하고 apply-local payload로 전파하도록 했다.

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
