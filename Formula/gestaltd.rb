# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.2/gestaltd-macos-arm64.tar.gz"
      sha256 "556edb1b11a2307d947334b3a69855b144742e691bca34793a14e606c8c039d4"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.2/gestaltd-macos-x86_64.tar.gz"
      sha256 "92a4825626962f51f04601ba01351e86f696fd9a39f992524e77e723c3df5e77"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.2/gestaltd-linux-arm64.tar.gz"
      sha256 "25529d458624fc3d7702486279e19fb42f373f22050a5e69800df86c968e1905"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.2/gestaltd-linux-x86_64.tar.gz"
      sha256 "cc853c73b94682ff6ebc5b86408e0dcab7fde84604b5bfa3934fa679dc0fe206"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
