# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.35"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.35/gestaltd-macos-arm64.tar.gz"
      sha256 "cbfbca226b9b6389ec2b48927dd48fd1f82d8230fe5fc89f7b9fea00e699398a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.35/gestaltd-macos-x86_64.tar.gz"
      sha256 "765332007b859f3e65830d0218b0d0ffa49b4d98f908428a92fd25810317650e"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.35/gestaltd-linux-arm64.tar.gz"
      sha256 "6c6a0e92817b433815bd6015bb93eb796d340d6bb1d0bf33522bbd308771eb4e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.35/gestaltd-linux-x86_64.tar.gz"
      sha256 "e1eefbeff3d157a8d129dd54e8001fa9a0bab18188c3d4e52d6c95379efb2242"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
