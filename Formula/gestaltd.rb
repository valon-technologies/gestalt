# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.22"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.22/gestaltd-macos-arm64.tar.gz"
      sha256 "b68289bec3ac2e4316059772bad25cf376ea3245114071171ce6d023bd6a2ff3"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.22/gestaltd-macos-x86_64.tar.gz"
      sha256 "e97e1e34eb84fdd3db3aff1a78e86642d7ca85c88ace7c536d81c78d0eacf29f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.22/gestaltd-linux-arm64.tar.gz"
      sha256 "6cec5853d28b86ca2c1768c8a2dbf7b4eeef79e8639960b77109e7cd8f0fabe4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.22/gestaltd-linux-x86_64.tar.gz"
      sha256 "d117ba4a0e6a074010d6125e1140fc6b0e65165a75593f127659ab056c11e831"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
