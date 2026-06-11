# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.15"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.15/gestaltd-macos-arm64.tar.gz"
      sha256 "a59e56e3a3834b33922d85924bcf60b9e47aef820268c014fe5d77da7e2fffb5"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.15/gestaltd-macos-x86_64.tar.gz"
      sha256 "43b7174ff6ccbb0fff4f705cea3378a4ce25cdfe392e7afefde59326cb3a8cb6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.15/gestaltd-linux-arm64.tar.gz"
      sha256 "2039b901846d87bdfff25d8f5daf03b4cd896bf072baf7ee98e29ae516a31969"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.15/gestaltd-linux-x86_64.tar.gz"
      sha256 "02dbb7fbbeb4c8a24601c4c8e6feb3914f7aafdf906e94f7471e7aab4b6f099e"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
