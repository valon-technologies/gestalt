# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.44"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.44/gestaltd-macos-arm64.tar.gz"
      sha256 "a171e0d08321e88c8816c130e03801f70b605752853298eaf17f8f8674117588"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.44/gestaltd-macos-x86_64.tar.gz"
      sha256 "481b3ed3c1e3d48de1b0f5fa5004f2fe08dce24f8caaa5198b79333984b1185c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.44/gestaltd-linux-arm64.tar.gz"
      sha256 "6b7d7b860958c5dba3ec9e0fe990968603af9f5b5ba79712f0c7341598309ebf"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.44/gestaltd-linux-x86_64.tar.gz"
      sha256 "157a7df2bdbc2054002b1da53e8a8bb2324d65d0dd58227958dcf5de934a7da6"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
