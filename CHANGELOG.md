# CHANGELOG

ABLESTACK API의 변경 이력은 이 파일에 기록한다. RPM 버전은 `VERSION` 파일을 기준으로 관리한다.

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
