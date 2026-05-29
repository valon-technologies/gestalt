# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.7"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.7/gestaltd-macos-arm64.tar.gz"
      sha256 "c421c6e0fc3819c3857d402321cc46f4ee80bb0f4039a61b6257bde603390aa6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.7/gestaltd-macos-x86_64.tar.gz"
      sha256 "55b670bb8f580f52db054f68f1a61abe772854850bf60dd00352f8170fac381e"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.7/gestaltd-linux-arm64.tar.gz"
      sha256 "da1c5ac5863e1b95d9c1f903e6c27f6d17e0fbd3207ece257e9c74489181dfa1"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.7/gestaltd-linux-x86_64.tar.gz"
      sha256 "523458bd3bbeadb1dbf88484c455fc081f2e250b7accd13cee3633dea92dbef1"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
