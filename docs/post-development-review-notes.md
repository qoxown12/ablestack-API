# Post-development Review Notes

작성일: 2026-05-26

## 배경

현재 단계에서는 주요 기능과 RPM 설치 구조가 어느 정도 정리되었으므로, 다음 단계는 동작 흐름을 유지하면서 코드 구조를 안전하게 정리하는 것이다.
이 문서는 바로 구현할 작업 목록이 아니라, 구조 변경을 어떤 순서와 기준으로 진행할지 정리한 계획이다.

## 유지해야 할 제약

아래 항목은 현재 제품/운영 호환성 때문에 제거 대상으로 보지 않는다.

- DB root 비밀번호 하드코딩은 제거하지 않는다.
- license 복호화에 사용하는 고정 password/salt 값은 제거하지 않는다.

대신 위 항목은 사용 범위를 좁히고, 로그/응답/프로세스 목록에 노출되지 않도록 관리한다.
예를 들어 DB 비밀번호 상수는 여러 파일에 흩어지지 않게 한 곳에서만 참조하고, license 복호화 고정값도 복호화 함수 내부에만 머물도록 한다.

## 구조 변경 원칙

- API path, request, response 형식은 구조 변경 중에는 유지한다.
- 파일 이동과 로직 변경을 한 번에 섞지 않는다.
- rename 작업 후에는 `rg`로 예전 경로/이름이 남았는지 확인한다.
- 각 단계마다 `gofmt`, 가능한 단위의 `go test`, RPM 스크립트 문법 검사를 실행한다.
- `/etc/ablestack` 경로 정책과 RPM 설치 흐름은 유지한다.
- `internal/handler/cube`에서 `internal/service/cube`로 옮기는 큰 작업은 나중 단계로 미룬다.

## 목표 구조

현재 단계에서 목표로 삼을 구조는 아래 정도가 적당하다.

```text
cmd/apiserver
  API 서버 진입점, route 등록, swagger 설정

internal/handler/cube
  HTTP 요청 bind, validation, response 반환
  기능별 파일명은 ccvm_*, scvm_*, gfs_*, pcs_* 기준으로 유지

internal/model/cube
  기존 명령 실행, 상태 조회, 도메인 모델 유지

internal/service
  controller, clusterconfig 같은 공유 서비스 유지
  service/cube 분리는 이후 별도 작업으로 진행

internal/infra
  libvirt, utils, ssh, command/file helper 같은 외부 시스템 접근 코드

packaging, scripts, docs
  RPM, systemd, 설정 파일 설치, 운영 문서 관리
```

## 단계별 진행 계획

1. 현재 상태 고정

- 구조 변경 전 현재 동작 기준을 정리한다.
- `go test ./internal/model/... ./internal/service/... ./docs`를 기준 테스트로 둔다.
- `handler/cube`와 `cmd/apiserver`는 로컬에 `pkg-config`, `libvirt-devel`이 있어야 빌드 가능하므로 환경 의존 테스트로 분리한다.

2. 파일명 정리

- `create_ccvm_cloudinit.go` 같은 동사형 파일명은 `ccvm_cloudinit.go`처럼 기능 기준으로 정리한다.
- `create_scvm_cloudinit.go`, `create_ccvm_xml.go`, `create_scvm_xml.go`도 같은 기준을 적용한다.
- route 함수명은 외부 API 문서와 연결되므로 불필요하게 바꾸지 않는다.

3. handler 공통 helper 축소

- `common.go`가 계속 커지면 역할 기준으로 분리한다.
- 후보 파일은 `paths.go`, `response.go`, `command.go`, `cluster.go` 정도로 둔다.
- 이 단계에서는 package를 나누지 않고 `internal/handler/cube` 안에서만 파일을 나눠 위험을 줄인다.

4. infra helper 분리

- libvirt 직접 호출, guest-agent 호출, domain XML parsing은 `internal/infra/libvirt`에서 관리한다.
- handler는 `libvirtinfra` 공개 함수만 사용한다.
- SSH, 파일 복사, command 실행도 장기적으로는 `internal/infra` 하위로 분리할 수 있다.

5. 보안 보호 추가

- `/api/v1` 전체에 인증/인가 미들웨어를 추가한다.
- 웹/프론트엔드 호출은 Linux 계정 로그인 후 발급받은 `Authorization: Bearer <access-token>` 방식으로 보호한다.
- 호스트 간 fan-out 요청은 `X-Cube-Internal-Token` 같은 내부 토큰으로 보호한다.
- `X-Cube-...-Local` 계열 헤더가 붙은 요청은 내부 토큰이 맞을 때만 실행한다.
- 운영 환경에서는 CORS `Access-Control-Allow-Origin: *`를 제거하고 허용 origin을 명시한다.

예상 환경 변수:

```env
CUBE_INTERNAL_TOKEN=<cluster-internal-token>
ABLESTACK_API_PORT=8090
```

6. 안정성 문제 정리

- 10초마다 모든 모니터를 goroutine으로 중복 실행하는 구조에 중복 실행 방지, timeout, context 기반 종료 처리를 추가한다.
- neighbor 삭제 로직에서 slice 순회 중 삭제로 인한 skip/panic 가능성을 점검한다.
- neighbor 호출 포트는 `ABLESTACK_API_PORT` 또는 설정값 기반으로 통일한다.
- PCS, virsh, qemu-img, rbd 같은 외부 명령은 timeout과 stderr/stdout 처리 기준을 통일한다.

7. 시스템 변경 side effect 축소

- cluster apply의 `check`, `remove`, `reset` 같은 액션에서 `/etc/fstab`, `/hugepages`, `mount -a`가 의도치 않게 실행되지 않도록 분리한다.
- API action은 요청한 변경만 수행하도록 정리한다.
- 실제 시스템 파일을 변경하는 API는 dry-run 또는 사전 검증 단계를 둘지 검토한다.

8. RPM/운영 검증

- RPM 설치 시 `/etc/ablestack` 하위 설정 파일이 유지되는지 확인한다.
- 기존 JSON은 값 유지, 누락 key만 추가되는지 확인한다.
- 서비스 enable/start, firewalld port open, upgrade restart 흐름을 설치/업데이트 시나리오로 나눠 확인한다.

## 우선순위

가장 먼저 해야 할 일은 큰 구조 변경이 아니라 안전한 경계 정리다.

1. handler 파일명과 공통 helper 정리
2. `internal/infra`로 외부 시스템 접근 helper 이동
3. 인증/내부 토큰 보호 추가
4. controller/monitor 안정성 정리
5. 시스템 변경 side effect 축소
6. 이후 `internal/service/cube` 분리 검토

## 운영 전 확인

- 3대 호스트 사이에 `8090/tcp` 통신이 가능한지 확인한다.
- 호스트 간 API fan-out을 계속 쓸 경우 내부 토큰 방식으로 충분하다.
- 네트워크 정책상 호스트 간 `8090/tcp`가 막혀 있다면 인증 문제가 아니라 통신 경로 문제이므로 SSH, agent pull, 메시지 큐 같은 별도 구조를 검토한다.
- DB 비밀번호와 license 복호화 고정값은 제거 대상이 아니므로, 노출 경로를 줄이는 방향으로만 관리한다.
