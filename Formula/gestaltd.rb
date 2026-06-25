# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.34"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.34/gestaltd-macos-arm64.tar.gz"
      sha256 "77acaddf50310ec1cfe2cf3e61965b56869b2b7af6c46f9b67c1b17aa4fe0eb5"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.34/gestaltd-macos-x86_64.tar.gz"
      sha256 "4fb878da474e9bf953faec3c301bb084829df6b345dec64d52551a8a7ffd973f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.34/gestaltd-linux-arm64.tar.gz"
      sha256 "b43f7858c735a6ab4ce0204563bf8e0487162a9bb30f16fd658b38314b952358"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.34/gestaltd-linux-x86_64.tar.gz"
      sha256 "b6016c69df4865ec7df494a8188788af0c9ef4ead8c019063e3f1882f4ad3267"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
