# Mobile Gateway redesign QA

- source visual truth path: `/Users/ghyeok/.codex/generated_images/019f595b-57a6-7c11-a7f3-10400513b5b4/exec-58f5a052-c46b-4d0b-8e0a-584f32695175.png`
- implementation screenshot path: `docs/design-audit/03-redesign-implementation.jpg`
- viewports: 390 × 844, 768 × 1024, 1440 × 1024
- state: 초기 설정, ClawOps 미연결, 블루투스 어댑터 없음, 서비스 정지

**Full-view comparison evidence**

선택 시안과 구현 캡처를 같은 1440 × 1024 상태로 함께 열어 비교했다. 상단 상태 표시, 4단계 가로 진행 구조, 좌측 단일 작업 영역, 우측 요약 패널, 하단 고급 설정/진단 로그 구조와 전체 비율이 일치한다.

**Focused region comparison evidence**

별도 확대 비교는 필요하지 않았다. 1440 × 1024 원본에서 제목, 도움말, 입력 라벨, 상태 문구, 버튼 텍스트를 모두 읽을 수 있었고 핵심 폼 영역도 동일 상태로 노출되었다.

**Findings**

- P0/P1/P2 없음.
- Fonts and typography: 시스템 UI 글꼴과 Noto Sans KR 폴백으로 시안의 중립적인 산세리프 계층을 재현했다. 제목 크기와 본문 줄 높이, 작은 상태 텍스트 대비를 확인했다.
- Spacing and layout rhythm: 4단계 진행 바, 1fr/350px 작업 영역, 입력 간격, 요약 패널 구분선을 시안 비율에 맞췄다.
- Colors and visual tokens: 따뜻한 배경, 흰 작업 면, graphite 본문, indigo 강조색, semantic 상태색을 토큰화했다.
- Image quality and asset fidelity: 참조 화면에 사진·일러스트·로고 자산이 없어 누락된 이미지 자산이 없다. 표준 상태 점 외에 대체 그래픽을 만들지 않았다.
- Copy and content: 실제 제품 계약인 등록 토큰, Bluetooth HFP, 회선 설정, 서비스 시작 흐름에 맞춰 한국어 문구를 정리했다.
- Accessibility and interaction: 단계 버튼의 키보드 접근, 현재 단계 속성, 폼 라벨, 필수 입력, 상태 `aria-live`, focus-visible을 확인했다.

**Primary interactions tested**

- 네 단계 탭 전환
- 초기 등록 폼의 필수 입력 노출
- 진단 로그 자동 갱신
- 브라우저 콘솔 오류 없음
- 완료된 네 단계의 체크 상태와 잠금 해제
- 정상 운영 배너와 등록 완료 화면
- 설정 수정 모드, 미저장 변경 경고, 변경 취소
- 운영 중 저장 후 재시작 필요 상태
- 390px와 768px에서 가로 오버플로 없음
- Chrome Lighthouse 모바일: Accessibility 100, Best Practices 100, SEO 100, Agentic Browsing 100

**Comparison history**

- 최초 PNG 캡처에서 Chromium GPU 검은 영역이 발견되어 JPEG 캡처로 변경했다.
- 변경 후 캡처에서 검은 영역이 제거되었고 P0/P1/P2 시각 차이가 남지 않았다.
- 첫 모바일 캡처에서 단계 라벨 줄바꿈을 발견해 4열 compact stepper로 수정했다.
- Lighthouse에서 작은 보조 문구의 대비 문제를 발견해 `--muted` 색상을 조정하고 재검사했다. 최종 실패 항목은 0개다.

**Follow-up Polish**

- 실제 블루투스 어댑터와 등록 토큰이 있는 장비에서 성공 상태 스크린샷을 추가할 수 있다.

final result: passed
